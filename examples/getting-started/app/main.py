from flask import Flask
import os

app = Flask(__name__)
pod_name = "unknown"
pod_namespace = "unknown"
cluster_id = os.environ.get("MEMBER_CLUSTER_ID")

@app.route("/")
def hello_world():
    return f"Hello World! (Pod: {pod_namespace}/{pod_name}, Cluster: {cluster_id})"

if __name__ == "__main__":
    try:
        with open("/etc/podinfo/name", "rt") as f:
            pod_name = f.read()
        with open("/etc/podinfo/namespace", "rt") as f:
            pod_namespace = f.read()
    except:
        pass

    app.run(host="0.0.0.0", port=8080, debug=True)
