#!/usr/bin/env python3
"""Verify the exact OpenVEX boundary and its source/runtime evidence."""

from __future__ import annotations

import argparse
import json
import pathlib
import subprocess
import sys
from typing import Any


ROOT = pathlib.Path(__file__).resolve().parent.parent
GENERATOR = ROOT / "scripts" / "generate-openvex"

GO_RESTFUL_PURL = (
    "pkg:golang/github.com/emicklei/go-restful@v1.2-75-gb14c3a9"
)
PROMETHEUS_PURL = (
    "pkg:golang/github.com/prometheus/client_golang@0.7.0-87-g28be158"
)
KUBERNETES_PURL = "pkg:golang/k8s.io/kubernetes@v1.4.4"
DOCKER_PURL = "pkg:golang/github.com/docker/docker@v28.5.2%2Bincompatible"
GO_STDLIB_PURL = "pkg:golang/stdlib@v1.26.5"
LINUX_HEADERS_PURL = (
    "pkg:deb/ubuntu/linux-libc-dev@7.0.0-29.29"
    "?arch=amd64&distro=ubuntu-26.04"
)

EXPECTED_SOURCE = {
    ("CVE-2019-11253", KUBERNETES_PURL),
    ("CVE-2020-8558", KUBERNETES_PURL),
    ("CVE-2021-25741", KUBERNETES_PURL),
    ("CVE-2022-1996", GO_RESTFUL_PURL),
    ("CVE-2022-21698", PROMETHEUS_PURL),
    ("CVE-2023-3676", KUBERNETES_PURL),
    ("CVE-2023-3955", KUBERNETES_PURL),
    ("CVE-2023-5528", KUBERNETES_PURL),
    ("CVE-2024-0793", KUBERNETES_PURL),
    ("CVE-2024-10220", KUBERNETES_PURL),
    ("CVE-2024-5321", KUBERNETES_PURL),
}
EXPECTED_DAPPER_NON_KERNEL = {
    ("CVE-2026-34040", DOCKER_PURL),
    ("CVE-2026-39821", GO_STDLIB_PURL),
    ("CVE-2026-41567", DOCKER_PURL),
    ("CVE-2026-42306", DOCKER_PURL),
    ("CVE-2026-46600", GO_STDLIB_PURL),
}

FORBIDDEN_DEPENDENCY_FRAGMENTS = (
    "github.com/gogo/protobuf/plugin/",
    "k8s.io/kubernetes/pkg/apiserver",
    "k8s.io/kubernetes/pkg/controller/podautoscaler",
    "k8s.io/kubernetes/pkg/kubelet/kuberuntime",
    "k8s.io/kubernetes/pkg/kubelet/network",
    "k8s.io/kubernetes/pkg/proxy",
    "k8s.io/kubernetes/pkg/volume",
)
ALLOWED_KUBELET_DEPENDENCIES = {
    "k8s.io/kubernetes/pkg/kubelet/qos",
    "k8s.io/kubernetes/pkg/kubelet/types",
}


def load_json(path: pathlib.Path) -> dict[str, Any]:
    data = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(data, dict):
        raise AssertionError(f"{path}: expected a JSON object")
    return data


def expected_linux_pairs() -> set[tuple[str, str]]:
    path = ROOT / "security" / "dapper-linux-libc-dev-reviewed-cves.txt"
    return {
        (line.strip(), LINUX_HEADERS_PURL)
        for line in path.read_text(encoding="utf-8").splitlines()
        if line.strip()
    }


def validate_vex(path: pathlib.Path) -> dict[str, Any]:
    actual = load_json(path)
    generated = json.loads(
        subprocess.check_output([sys.executable, str(GENERATOR)], text=True)
    )
    assert actual == generated, "OpenVEX differs from the deterministic generator"
    assert actual["@context"] == "https://openvex.dev/ns/v0.2.0"
    assert actual["author"] == "PastureStack Security"
    assert actual["version"] == 1

    pairs: set[tuple[str, str]] = set()
    fixed: set[tuple[str, str]] = set()
    for item in actual["statements"]:
        products = item["products"]
        assert len(products) == 1
        pair = (item["vulnerability"]["name"], products[0]["@id"])
        assert pair not in pairs, f"duplicate OpenVEX statement: {pair}"
        pairs.add(pair)
        assert item["impact_statement"].strip()
        if item["status"] == "fixed":
            assert "justification" not in item
            fixed.add(pair)
        else:
            assert item["status"] == "not_affected"
            assert item["justification"] in {
                "vulnerable_code_not_present",
                "vulnerable_code_not_in_execute_path",
                "vulnerable_code_cannot_be_controlled_by_adversary",
            }

    expected_dapper = expected_linux_pairs() | EXPECTED_DAPPER_NON_KERNEL
    assert pairs == EXPECTED_SOURCE | expected_dapper
    assert fixed == {("CVE-2022-1996", GO_RESTFUL_PURL)}
    assert len(pairs) == 51
    return actual


def validate_source_boundary() -> None:
    cors = (
        ROOT
        / "vendor"
        / "github.com"
        / "emicklei"
        / "go-restful"
        / "cors_filter.go"
    ).read_text(encoding="utf-8")
    assert 'if !strings.HasPrefix(each, "^") {' in cors
    assert 'each = fmt.Sprintf("^%s$", each)' in cors

    regression = (
        ROOT / "security" / "regression" / "go_restful_cors_test.go"
    ).read_text(encoding="utf-8")
    for required in (
        "https://example.com",
        "https://console.example.com",
        "https://example.com.attacker.invalid",
        "https://attacker.invalid/https://example.com",
    ):
        assert required in regression

    forbidden_calls = ("InstrumentHandler", "promhttp.")
    for path in ROOT.rglob("*.go"):
        relative = path.relative_to(ROOT)
        if relative.parts[0] in {"vendor", "security"}:
            continue
        source = path.read_text(encoding="utf-8", errors="replace")
        for call in forbidden_calls:
            assert call not in source, f"affected Prometheus call in {relative}: {call}"


def normalize_dependency(line: str) -> str:
    marker = "/vendor/"
    value = line.strip().replace("\\", "/")
    if marker in value:
        return value.split(marker, 1)[1]
    return value


def validate_dependencies(path: pathlib.Path) -> None:
    dependencies = {
        normalize_dependency(line)
        for line in path.read_text(encoding="utf-8").splitlines()
        if line.strip()
    }
    assert "github.com/emicklei/go-restful" in dependencies
    assert "github.com/prometheus/client_golang/prometheus" in dependencies
    assert ALLOWED_KUBELET_DEPENDENCIES <= dependencies

    unexpected_kubelet = {
        dependency
        for dependency in dependencies
        if dependency.startswith("k8s.io/kubernetes/pkg/kubelet/")
        and dependency not in ALLOWED_KUBELET_DEPENDENCIES
    }
    assert not unexpected_kubelet, unexpected_kubelet
    for dependency in dependencies:
        assert not any(
            fragment in dependency for fragment in FORBIDDEN_DEPENDENCY_FRAGMENTS
        ), dependency


def purl_for(result: dict[str, Any], vulnerability: dict[str, Any]) -> str:
    identifier = vulnerability.get("PkgIdentifier") or {}
    if identifier.get("PURL"):
        return identifier["PURL"]
    for package in result.get("Packages") or []:
        if (
            package.get("Name") == vulnerability.get("PkgName")
            and package.get("Version") == vulnerability.get("InstalledVersion")
        ):
            return (package.get("Identifier") or {}).get("PURL", "")
    raise AssertionError(
        "No PURL for "
        f"{vulnerability.get('VulnerabilityID')} {vulnerability.get('PkgName')}"
    )


def critical_high_pairs(path: pathlib.Path) -> set[tuple[str, str]]:
    report = load_json(path)
    pairs: set[tuple[str, str]] = set()
    for result in report.get("Results") or []:
        for vulnerability in result.get("Vulnerabilities") or []:
            if vulnerability.get("Severity") not in {"CRITICAL", "HIGH"}:
                continue
            pairs.add(
                (
                    vulnerability["VulnerabilityID"],
                    purl_for(result, vulnerability),
                )
            )
    return pairs


def critical_high_and_secrets(path: pathlib.Path) -> tuple[int, int]:
    report = load_json(path)
    vulnerabilities = 0
    secrets = 0
    for result in report.get("Results") or []:
        vulnerabilities += sum(
            vulnerability.get("Severity") in {"CRITICAL", "HIGH"}
            for vulnerability in result.get("Vulnerabilities") or []
        )
        secrets += len(result.get("Secrets") or [])
    return vulnerabilities, secrets


def validate_reports(args: argparse.Namespace) -> None:
    report_paths = (
        args.raw_source,
        args.raw_dapper,
        args.applicable_source,
        args.applicable_dapper,
    )
    if not any(report_paths):
        return
    assert all(report_paths), "all raw/applicable source and Dapper reports are required"
    assert critical_high_pairs(args.raw_source) == EXPECTED_SOURCE
    assert critical_high_pairs(args.raw_dapper) == (
        expected_linux_pairs() | EXPECTED_DAPPER_NON_KERNEL
    )
    assert critical_high_and_secrets(args.applicable_source) == (0, 0)
    assert critical_high_and_secrets(args.applicable_dapper) == (0, 0)

    for path in (args.raw_product, args.raw_service, args.raw_sidecar):
        if path is not None:
            assert critical_high_and_secrets(path) == (0, 0), path


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--vex", type=pathlib.Path, required=True)
    parser.add_argument("--dependencies", type=pathlib.Path)
    parser.add_argument("--raw-source", type=pathlib.Path)
    parser.add_argument("--raw-dapper", type=pathlib.Path)
    parser.add_argument("--applicable-source", type=pathlib.Path)
    parser.add_argument("--applicable-dapper", type=pathlib.Path)
    parser.add_argument("--raw-product", type=pathlib.Path)
    parser.add_argument("--raw-service", type=pathlib.Path)
    parser.add_argument("--raw-sidecar", type=pathlib.Path)
    args = parser.parse_args()

    validate_vex(args.vex)
    validate_source_boundary()
    if args.dependencies is not None:
        validate_dependencies(args.dependencies)
    validate_reports(args)
    print("LOAD_BALANCER_OPENVEX_OK statements=51 source=11 dapper=40")


if __name__ == "__main__":
    main()
