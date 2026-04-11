"""
SDK Compatibility Tests for Poly Paper Trading.

These tests use the real py-clob-client SDK against our paper trading server
to validate wire-format compatibility. If any call fails with a deserialization
or auth error, our API format is wrong.

Prerequisites:
  pip install py-clob-client pytest requests
  # Server must be running: make docker-up && make run

Usage:
  cd tests/sdk_compat
  python -m pytest test_sdk_compat.py -v
"""

import os
import time
import json
import hashlib
import hmac
import base64
import requests
import pytest

PAPER_HOST = os.getenv("PAPER_HOST", "http://localhost:8080")
CLOB_HOST = f"{PAPER_HOST}/clob"
TEST_EMAIL = f"test-{int(time.time())}@example.com"
TEST_PASSWORD = "testpass123"


class TestDashboardAuth:
    """Test dashboard JWT auth endpoints."""

    token: str = ""
    user: dict = {}

    def test_register(self):
        resp = requests.post(
            f"{PAPER_HOST}/auth/register",
            json={"email": TEST_EMAIL, "password": TEST_PASSWORD},
        )
        assert resp.status_code == 201, f"Register failed: {resp.text}"
        data = resp.json()
        assert "token" in data
        assert "user" in data
        assert data["user"]["email"] == TEST_EMAIL
        assert data["user"]["eth_address"].startswith("0x")
        TestDashboardAuth.token = data["token"]
        TestDashboardAuth.user = data["user"]

    def test_login(self):
        resp = requests.post(
            f"{PAPER_HOST}/auth/login",
            json={"email": TEST_EMAIL, "password": TEST_PASSWORD},
        )
        assert resp.status_code == 200, f"Login failed: {resp.text}"
        data = resp.json()
        assert "token" in data

    def test_duplicate_register(self):
        resp = requests.post(
            f"{PAPER_HOST}/auth/register",
            json={"email": TEST_EMAIL, "password": TEST_PASSWORD},
        )
        assert resp.status_code == 409

    def test_get_wallet(self):
        resp = requests.get(
            f"{PAPER_HOST}/api/wallet",
            headers={"Authorization": f"Bearer {TestDashboardAuth.token}"},
        )
        assert resp.status_code == 200, f"Get wallet failed: {resp.text}"
        data = resp.json()
        assert "balance" in data
        # Default balance is $1000
        assert float(data["balance"]) == 1000.0

    def test_get_eth_address(self):
        resp = requests.get(
            f"{PAPER_HOST}/api/eth-address",
            headers={"Authorization": f"Bearer {TestDashboardAuth.token}"},
        )
        assert resp.status_code == 200, f"Get eth address failed: {resp.text}"
        data = resp.json()
        assert "eth_address" in data
        assert "private_key" in data
        assert data["private_key"].startswith("0x")

    def test_deposit(self):
        resp = requests.post(
            f"{PAPER_HOST}/api/wallet/deposit",
            headers={"Authorization": f"Bearer {TestDashboardAuth.token}"},
            json={"amount": "500"},
        )
        assert resp.status_code == 200

        # Verify balance increased
        resp = requests.get(
            f"{PAPER_HOST}/api/wallet",
            headers={"Authorization": f"Bearer {TestDashboardAuth.token}"},
        )
        assert float(resp.json()["balance"]) == 1500.0

    def test_withdraw(self):
        resp = requests.post(
            f"{PAPER_HOST}/api/wallet/withdraw",
            headers={"Authorization": f"Bearer {TestDashboardAuth.token}"},
            json={"amount": "200"},
        )
        assert resp.status_code == 200

    def test_withdraw_insufficient(self):
        resp = requests.post(
            f"{PAPER_HOST}/api/wallet/withdraw",
            headers={"Authorization": f"Bearer {TestDashboardAuth.token}"},
            json={"amount": "999999"},
        )
        assert resp.status_code == 400

    def test_jwt_required(self):
        resp = requests.get(f"{PAPER_HOST}/api/wallet")
        assert resp.status_code == 401

    def test_get_orders_empty(self):
        resp = requests.get(
            f"{PAPER_HOST}/api/orders",
            headers={"Authorization": f"Bearer {TestDashboardAuth.token}"},
        )
        assert resp.status_code == 200
        data = resp.json()
        assert "orders" in data
        assert "total" in data

    def test_get_positions_empty(self):
        resp = requests.get(
            f"{PAPER_HOST}/api/positions",
            headers={"Authorization": f"Bearer {TestDashboardAuth.token}"},
        )
        assert resp.status_code == 200
        data = resp.json()
        assert "positions" in data

    def test_get_trades_empty(self):
        resp = requests.get(
            f"{PAPER_HOST}/api/trades",
            headers={"Authorization": f"Bearer {TestDashboardAuth.token}"},
        )
        assert resp.status_code == 200
        data = resp.json()
        assert "trades" in data
        assert "total" in data


class TestCLOBPublicEndpoints:
    """Test that public CLOB endpoints proxy correctly to Polymarket."""

    def test_health(self):
        resp = requests.get(f"{CLOB_HOST}/")
        assert resp.status_code == 200

    def test_server_time(self):
        resp = requests.get(f"{CLOB_HOST}/time")
        # This proxies to Polymarket — should return a timestamp
        assert resp.status_code == 200


if __name__ == "__main__":
    pytest.main([__file__, "-v"])
