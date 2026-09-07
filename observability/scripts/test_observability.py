#!/usr/bin/env python3
"""Regression checks for the bounded Grafana provisioning changes."""
from __future__ import annotations

import json
import re
import unittest
from pathlib import Path

import yaml


ROOT = Path(__file__).resolve().parents[1]
ALERTING = ROOT / "config" / "grafana" / "provisioning" / "alerting"
DASHBOARDS = ROOT / "config" / "grafana" / "provisioning" / "dashboards"
RAW_TEMPLATE = ROOT / "config" / "grafana-metal-discord-template.txt"
PROVISIONED_TEMPLATE = ALERTING / "metal-templates.yaml"


def load_rules(name: str) -> list[dict]:
    document = yaml.safe_load((ALERTING / name).read_text(encoding="utf-8"))
    return [rule for group in document["groups"] for rule in group["rules"]]


class ObservabilityProvisioningTest(unittest.TestCase):
    def test_vpn_dashboard_covers_alert_metrics(self) -> None:
        dashboard = json.loads((DASHBOARDS / "my-utils" / "vpn-health.json").read_text())
        panels = {panel["id"]: panel for panel in dashboard["panels"]}
        self.assertEqual(dashboard["uid"], "myutils-vpn-health")
        self.assertEqual(set(panels), set(range(1, 9)))
        expressions = "\n".join(target["expr"] for panel in panels.values() for target in panel["targets"])
        for metric in (
            "collection_success",
            "relay_ready",
            "agent_last_seen_timestamp_seconds",
            "routing_healthy",
            "exit_healthy",
            "exit_selected",
            "exit_preference",
            "route_packet_loss_percent",
        ):
            self.assertIn(f"myutils_wireguard_{metric}", expressions)

    def test_api_dashboard_titles_are_folder_neutral(self) -> None:
        logs = json.loads((DASHBOARDS / "my-utils" / "my-utils-api-logs.json").read_text())
        metrics = json.loads((DASHBOARDS / "my-utils" / "my-utils-api-metrics.json").read_text())
        self.assertEqual(logs["title"], "API Logs")
        self.assertEqual(metrics["title"], "API Metrics")
        self.assertNotIn("My Utils", logs["title"])
        self.assertNotIn("My Utils", metrics["title"])

    def test_vpn_rules_have_distinct_panel_links_and_outage_policy(self) -> None:
        rules = load_rules("vpn-alert-rules.yaml")
        self.assertEqual(len(rules), 8)
        self.assertEqual({rule["panelId"] for rule in rules}, set(range(1, 9)))
        for rule in rules:
            annotations = rule["annotations"]
            self.assertEqual(rule["dashboardUid"], "myutils-vpn-health")
            self.assertEqual(annotations["__dashboardUid__"], "myutils-vpn-health")
            self.assertEqual(annotations["__panelId__"], str(rule["panelId"]))
            self.assertIn("myutils-vpn-health", annotations["dashboard_url"])
            self.assertIn(f"viewPanel={rule['panelId']}", annotations["panel_url"])
            if rule["panelId"] > 1:
                self.assertIn("urlquery", annotations["panel_url"])
            self.assertEqual(annotations["panel_url"], annotations["runbook_url"])
            self.assertEqual(rule["execErrState"], "KeepLast")
        self.assertEqual(rules[0]["noDataState"], "Alerting")
        self.assertEqual(rules[1]["noDataState"], "KeepLast")
        self.assertTrue(all(rule["noDataState"] == "OK" for rule in rules[2:]))

    def test_metal_rules_link_to_semantic_panels(self) -> None:
        rules = load_rules("metal-alert-rules.yaml")
        by_uid = {rule["uid"]: rule for rule in rules}
        expected = {
            "efqg6umajocg0c": ("rYdddlPWk", 157),
            "dfqg6wqbqesxse": ("rYdddlPWk", 77),
            "cfqg6wy89uewwf": ("rYdddlPWk", 78),
            "afqg6x9q6ipkwe": ("rYdddlPWk", 152),
        }
        self.assertEqual(set(by_uid), set(expected))
        document = yaml.safe_load((ALERTING / "metal-alert-rules.yaml").read_text())
        self.assertEqual(document["deleteRules"], [{"orgId": 1, "uid": "bfqg6service01"}])
        for uid, (dashboard_uid, panel_id) in expected.items():
            rule = by_uid[uid]
            annotations = rule["annotations"]
            self.assertEqual((rule["dashboardUid"], rule["panelId"]), (dashboard_uid, panel_id))
            self.assertEqual(annotations["__dashboardUid__"], dashboard_uid)
            self.assertEqual(annotations["__panelId__"], str(panel_id))
            self.assertIn(f"viewPanel={panel_id}", annotations["panel_url"])
            self.assertIn("var-host=", annotations["dashboard_url"])
            self.assertIn("var-node=", annotations["dashboard_url"])
            expected_exec_state = "OK" if uid == "efqg6umajocg0c" else "KeepLast"
            self.assertEqual(rule["execErrState"], expected_exec_state)
        baseline = by_uid["efqg6umajocg0c"]
        self.assertEqual((baseline["for"], baseline["noDataState"], baseline["execErrState"]), ("3m", "OK", "OK"))

    def test_template_is_byte_identical_and_webhook_safe(self) -> None:
        raw = RAW_TEMPLATE.read_text(encoding="utf-8")
        lines = PROVISIONED_TEMPLATE.read_text(encoding="utf-8").splitlines()
        start = lines.index("    template: |") + 1
        provisioned = "\n".join(line[6:] for line in lines[start:]) + "\n"
        self.assertEqual(raw, provisioned)
        self.assertNotIn("/alerting/", raw)
        self.assertIn("coll.Dict", raw)
        self.assertIn("coll.Slice", raw)
        self.assertIn("coll.Append", raw)
        self.assertIn("data.ToJSON", raw)
        self.assertIn("tmpl.Exec", raw)
        self.assertRegex(raw, r'printf "%.180s"')
        self.assertRegex(raw, r'printf "%.500s')
        self.assertIn("**Дашборд:**", raw)
        self.assertIn("**Панель:**", raw)

    def test_mutation_scripts_are_scoped(self) -> None:
        apply_script = (ROOT / "scripts" / "apply-metal-discord-template.py").read_text()
        retire_script = (ROOT / "scripts" / "retire-ruscrafting-alerts.py").read_text()
        sync_script = (ROOT / "sync-to-server.sh").read_text()
        self.assertIn('METAL_CONTACT_POINT_UID = "bfmetal6vcguq68c"', apply_script)
        self.assertIn('"type": "webhook"', apply_script)
        self.assertIn('"maxAlerts": 5', apply_script)
        self.assertIn('"payload": {"template":', apply_script)
        self.assertIn("HTTP {error.code} {error.reason}", apply_script)
        self.assertNotIn("for contact_point in contact_points", apply_script)
        self.assertNotIn("retire_legacy_rules", apply_script)
        self.assertEqual(set(re.findall(r'"(inf\d{5,})"', retire_script)), {
            "inf00001", "inf00002", "inf00003", "inf00004",
        })
        self.assertIn('sys.argv[1:] != ["--confirm"]', retire_script)
        self.assertNotIn("startswith", retire_script)
        self.assertIn("HTTP {error.code} {error.reason}", retire_script)
        self.assertNotIn("retire-ruscrafting-alerts.py\\n", sync_script)
        self.assertIn("--exclude 'grafana/provisioning/dashboards/'", sync_script)
        self.assertIn("--exclude 'grafana/dashboards/'", sync_script)
        self.assertIn("/dashboards/json/my-utils", sync_script)
        self.assertIn("/dashboards/json/ruscrafting", sync_script)
        self.assertIn("provisioning/dashboards/my-utils/", sync_script)
        self.assertNotIn('provisioning/dashboards/dashboards.yml" "${HOST}', sync_script)


if __name__ == "__main__":
    unittest.main()
