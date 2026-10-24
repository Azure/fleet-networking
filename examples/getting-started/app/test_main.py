import json
import unittest
from unittest.mock import patch

import main


class PrivateHTTPProofTests(unittest.TestCase):
    def test_existing_response_and_fresh_host_identity(self):
        with patch.object(main, "proof_id", "unique-proof"):
            response = main.app.test_client().get(
                "/?proof=fresh-request", headers={"Host": "incoming.example"}
            )
        self.assertEqual(response.status_code, 200)
        body = response.get_json()
        self.assertEqual(body["message"], "Hello World!")
        self.assertEqual(body["cluster_id"], main.cluster_id)
        self.assertEqual(body["host"], "incoming.example")
        self.assertEqual(body["request_id"], "fresh-request")
        self.assertEqual(body["proof_id"], "unique-proof")

    def test_probe_log_records_observed_host_and_method(self):
        with patch.object(main, "proof_id", "unique-proof"):
            with self.assertLogs(main.app.logger, level="WARNING") as captured:
                response = main.app.test_client().get(
                    "/", headers={"Host": "probe.example", "User-Agent": "Edge Health Probe"}
                )
        self.assertEqual(response.status_code, 200)
        record = json.loads(captured.records[0].getMessage())
        self.assertEqual(
            record,
            {
                "fleet_private_http_proof": "unique-proof",
                "method": "GET",
                "path": "/",
                "host": "probe.example",
                "user_agent": "Edge Health Probe",
            },
        )

    def test_ordinary_sample_does_not_enable_proof_logging(self):
        with patch.object(main, "proof_id", ""):
            with patch.object(main.app.logger, "warning") as warning:
                response = main.app.test_client().get("/")
        self.assertEqual(response.status_code, 200)
        warning.assert_not_called()


if __name__ == "__main__":
    unittest.main()
