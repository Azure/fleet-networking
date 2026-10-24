from flask import Flask, jsonify, request
import json
import os

app = Flask(__name__)
pod_name = "unknown"
pod_namespace = "unknown"
cluster_id = os.environ.get("MEMBER_CLUSTER_ID", "unknown")
proof_id = os.environ.get("FLEET_PROOF_ID", "")

@app.route("/")
def hello_world():
    if proof_id:
        app.logger.warning(json.dumps({
            "fleet_private_http_proof": proof_id,
            "method": request.method,
            "path": request.path,
            "host": request.host,
            "user_agent": request.user_agent.string
        }))
    return jsonify(
        message="Hello World!",
        pod_name=pod_name,
        pod_namespace=pod_namespace,
        cluster_id=cluster_id,
        host=request.host,
        request_id=request.args.get("proof", ""),
        proof_id=proof_id
    )

if __name__ == "__main__":
    try:
        with open("/etc/podinfo/name", "rt") as f:
            pod_name = f.read()
        with open("/etc/podinfo/namespace", "rt") as f:
            pod_namespace = f.read()
    except:
        pass

    app.run(host="0.0.0.0", port=8080)
