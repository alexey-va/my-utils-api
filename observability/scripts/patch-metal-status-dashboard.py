#!/usr/bin/env python3
"""Patch Metal Status dashboard: unified job name + friendly host aliases."""
import copy
import json
import sqlite3
import sys

DB_PATH = sys.argv[1] if len(sys.argv) > 1 else "/data/grafana.db"
UID = "rYdddlPWk"

JOB_QUERY = "label_values(node_uname_info{job=\"node-exporter\"}, job)"
HOST_QUERY = "label_values(node_uname_info{job=\"node-exporter\"}, service_name)"
NODE_QUERY = "label_values(node_uname_info{job=\"node-exporter\",service_name=\"$host\"}, instance)"

HOST_TEMPLATE = {
    "current": {"selected": True, "text": "utils", "value": "utils"},
    "datasource": {"type": "prometheus", "uid": "${datasource}"},
    "definition": HOST_QUERY,
    "hide": 0,
    "includeAll": False,
    "label": "Host",
    "multi": False,
    "name": "host",
    "options": [],
    "query": {"query": HOST_QUERY, "refId": "Prometheus-host-Variable-Query"},
    "refresh": 1,
    "regex": "",
    "skipUrlSync": False,
    "sort": 1,
    "type": "query",
}


def patch_variables(spec: dict) -> None:
    templating = spec.setdefault("templating", {})
    variables = templating.setdefault("list", [])

    by_name = {var.get("name"): var for var in variables}

    if "host" not in by_name:
        insert_at = 1 if variables else 0
        variables.insert(insert_at, copy.deepcopy(HOST_TEMPLATE))
        by_name["host"] = variables[insert_at]

    host_var = by_name["host"]
    host_var["label"] = "Host"
    host_var["hide"] = 0
    host_var["definition"] = HOST_QUERY
    host_var["query"] = {"query": HOST_QUERY, "refId": "Prometheus-host-Variable-Query"}
    host_var["regex"] = ""
    host_var["current"] = {"selected": True, "text": "utils", "value": "utils"}

    job_var = by_name.get("job")
    if job_var:
        job_var["definition"] = JOB_QUERY
        job_var["query"]["query"] = JOB_QUERY
        job_var["regex"] = ""
        job_var["hide"] = 2
        job_var["current"] = {
            "selected": True,
            "text": "node-exporter",
            "value": "node-exporter",
        }

    node_var = by_name.get("node")
    if node_var:
        node_var["definition"] = NODE_QUERY
        node_var["query"]["query"] = NODE_QUERY
        node_var["label"] = "Instance"
        node_var["hide"] = 2
        node_var["regex"] = ""
        node_var["current"] = {
            "selected": True,
            "text": "127.0.0.1:9100",
            "value": "127.0.0.1:9100",
        }


def main() -> None:
    con = sqlite3.connect(DB_PATH)
    row = con.execute("SELECT value FROM resource WHERE name=?", (UID,)).fetchone()
    if not row:
        raise SystemExit(f"dashboard {UID} not found")

    data = json.loads(row[0])
    spec = data.get("spec", data)
    patch_variables(spec)

    if "spec" in data:
        data["spec"] = spec
        payload = json.dumps(data)
    else:
        payload = json.dumps(spec)

    con.execute("UPDATE resource SET value=? WHERE name=?", (payload, UID))
    con.commit()
    print("patched", UID)


if __name__ == "__main__":
    main()
