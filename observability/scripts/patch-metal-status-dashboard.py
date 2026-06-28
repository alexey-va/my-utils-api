#!/usr/bin/env python3
"""Patch Metal Status dashboard variables for unified job=node-exporter."""
import json
import sqlite3
import sys

DB_PATH = sys.argv[1] if len(sys.argv) > 1 else "/data/grafana.db"
UID = "rYdddlPWk"

JOB_QUERY = "label_values(node_uname_info{job=\"node-exporter\"}, job)"
NODE_QUERY = "label_values(node_uname_info{job=\"node-exporter\"}, instance)"

con = sqlite3.connect(DB_PATH)
row = con.execute("SELECT value FROM resource WHERE name=?", (UID,)).fetchone()
if not row:
    raise SystemExit(f"dashboard {UID} not found")

data = json.loads(row[0])
spec = data.get("spec", data)
for var in spec.get("templating", {}).get("list", []):
    if var.get("name") == "job":
        var["definition"] = JOB_QUERY
        var["query"]["query"] = JOB_QUERY
        var["regex"] = ""
        var["current"] = {"selected": True, "text": "node-exporter", "value": "node-exporter"}
    if var.get("name") == "node":
        var["definition"] = NODE_QUERY
        var["query"]["query"] = NODE_QUERY
        var["current"] = {
            "selected": True,
            "text": "127.0.0.1:9100",
            "value": "127.0.0.1:9100",
        }

if "spec" in data:
    data["spec"] = spec
    payload = json.dumps(data)
else:
    payload = json.dumps(spec)

con.execute("UPDATE resource SET value=? WHERE name=?", (payload, UID))
con.commit()
print("patched", UID)
