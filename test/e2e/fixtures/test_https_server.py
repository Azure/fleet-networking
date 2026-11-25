from pathlib import Path
import ssl
import subprocess
import tempfile
import unittest
from unittest import mock

from https_server import validated_context


class CertificateTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary = tempfile.TemporaryDirectory()
        cls.addClassCleanup(cls.temporary.cleanup)
        cls.directory = Path(cls.temporary.name)
        cls.run_openssl("req", "-x509", "-newkey", "ec",
                        "-pkeyopt", "ec_paramgen_curve:prime256v1", "-nodes",
                        "-keyout", "ca.key", "-out", "ca.pem", "-days", "30",
                        "-subj", "/CN=Disposable fixture test CA",
                        "-addext", "basicConstraints=critical,CA:TRUE",
                        "-addext", "keyUsage=critical,keyCertSign,cRLSign")
        cls.run_openssl("req", "-new", "-newkey", "ec",
                        "-pkeyopt", "ec_paramgen_curve:prime256v1", "-nodes",
                        "-keyout", "intermediate.key", "-out", "intermediate.csr",
                        "-subj", "/CN=Disposable fixture intermediate")
        (cls.directory / "intermediate.ext").write_text(
            "basicConstraints=critical,CA:TRUE,pathlen:0\n"
            "keyUsage=critical,keyCertSign,cRLSign\n", encoding="ascii")
        cls.run_openssl("x509", "-req", "-in", "intermediate.csr",
                        "-CA", "ca.pem", "-CAkey", "ca.key", "-CAcreateserial",
                        "-out", "intermediate.pem", "-days", "30",
                        "-extfile", "intermediate.ext")
        for name, days, usage in (("valid", 14, "serverAuth"),
                                  ("short", 2, "serverAuth"),
                                  ("client", 14, "clientAuth")):
            cls.run_openssl("req", "-new", "-newkey", "ec",
                            "-pkeyopt", "ec_paramgen_curve:prime256v1", "-nodes",
                            "-keyout", name + ".key", "-out", name + ".csr",
                            "-subj", "/CN=origin.example.com")
            extensions = cls.directory / (name + ".ext")
            extensions.write_text(
                "basicConstraints=critical,CA:FALSE\n"
                "keyUsage=critical,digitalSignature\n"
                "extendedKeyUsage=" + usage + "\n"
                "subjectAltName=DNS:origin.example.com\n", encoding="ascii")
            cls.run_openssl("x509", "-req", "-in", name + ".csr",
                            "-CA", "intermediate.pem", "-CAkey", "intermediate.key", "-CAcreateserial",
                            "-out", name + ".pem", "-days", str(days),
                            "-extfile", name + ".ext")
            (cls.directory / (name + ".bundle")).write_bytes(
                (cls.directory / (name + ".pem")).read_bytes()
                + (cls.directory / "intermediate.pem").read_bytes()
                + (cls.directory / (name + ".key")).read_bytes())

    @classmethod
    def run_openssl(cls, *args):
        subprocess.run(["openssl", *args], cwd=cls.directory,
                       check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)

    def validate(self, name, hostname="origin.example.com", trusted=True):
        client = ssl.create_default_context(
            cafile=str(self.directory / "ca.pem") if trusted else None)
        return validated_context(self.directory / (name + ".bundle"), hostname, client)

    def test_valid_bundle_requires_no_external_listener(self):
        with mock.patch("socket.socket", side_effect=AssertionError("network access")):
            self.assertIsInstance(self.validate("valid"), ssl.SSLContext)

    def test_wrong_name(self):
        with self.assertRaises(ssl.SSLCertVerificationError):
            self.validate("valid", hostname="other.example.com")

    def test_untrusted_issuer(self):
        with self.assertRaises(ssl.SSLCertVerificationError):
            self.validate("valid", trusted=False)

    def test_server_usage(self):
        with self.assertRaises(ssl.SSLCertVerificationError):
            self.validate("client")

    def test_incomplete_chain(self):
        (self.directory / "incomplete.bundle").write_bytes(
            (self.directory / "valid.pem").read_bytes()
            + (self.directory / "valid.key").read_bytes())
        with self.assertRaises(ssl.SSLCertVerificationError):
            self.validate("incomplete")

    def test_lifetime_margin(self):
        with self.assertRaisesRegex(ValueError, "seven days"):
            self.validate("short")

    def test_expired_leaf(self):
        # Backdate only a disposable local unit-test certificate, never public issuance.
        config = self.directory / "ca.conf"
        config.write_text(
            "[ca]\ndefault_ca=issuer\n[issuer]\n"
            "database=index\nnew_certs_dir=.\ncertificate=ca.pem\n"
            "private_key=ca.key\nserial=serial\n"
            "default_md=sha256\npolicy=policy\nx509_extensions=server\n"
            "[policy]\ncommonName=supplied\n[server]\n"
            "basicConstraints=critical,CA:FALSE\n"
            "keyUsage=critical,digitalSignature\nextendedKeyUsage=serverAuth\n"
            "subjectAltName=DNS:origin.example.com\n", encoding="ascii")
        (self.directory / "index").touch()
        (self.directory / "serial").write_text("01\n", encoding="ascii")
        self.run_openssl("ca", "-batch", "-config", "ca.conf", "-in", "valid.csr",
                         "-out", "expired.pem", "-notext",
                         "-startdate", "20000101000000Z", "-enddate", "20010101000000Z")
        (self.directory / "expired.bundle").write_bytes(
            (self.directory / "expired.pem").read_bytes()
            + (self.directory / "valid.key").read_bytes())
        with self.assertRaises(ssl.SSLCertVerificationError):
            self.validate("expired")

    def test_mismatched_private_key(self):
        path = self.directory / "mismatch.bundle"
        path.write_bytes((self.directory / "valid.pem").read_bytes()
                         + (self.directory / "short.key").read_bytes())
        with self.assertRaises(ssl.SSLError):
            self.validate("mismatch")

    def test_public_certificate_is_not_a_serving_bundle(self):
        path = self.directory / "public.bundle"
        path.write_bytes((self.directory / "valid.pem").read_bytes())
        with self.assertRaises(ssl.SSLError):
            self.validate("public")


if __name__ == "__main__":
    unittest.main()
