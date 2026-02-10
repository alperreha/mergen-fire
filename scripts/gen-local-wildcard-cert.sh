#!/usr/bin/env bash
set -euo pipefail

OUT_DIR="${1:-./certs}"
CERT_DAYS="${CERT_DAYS:-3650}"
CERT_DOMAIN_PREFIX="${CERT_DOMAIN_PREFIX:-}"
CERT_DOMAIN_SUFFIX="${CERT_DOMAIN_SUFFIX:-localhost}"

CERT_DOMAIN_PREFIX="${CERT_DOMAIN_PREFIX#.}"
CERT_DOMAIN_PREFIX="${CERT_DOMAIN_PREFIX%.}"
CERT_DOMAIN_SUFFIX="${CERT_DOMAIN_SUFFIX#.}"
CERT_DOMAIN_SUFFIX="${CERT_DOMAIN_SUFFIX%.}"

if [[ -z "${CERT_DOMAIN_SUFFIX}" ]]; then
  echo "CERT_DOMAIN_SUFFIX cannot be empty" >&2
  exit 1
fi

if [[ -n "${CERT_DOMAIN_PREFIX}" ]]; then
  BASE_DOMAIN="${CERT_DOMAIN_PREFIX}.${CERT_DOMAIN_SUFFIX}"
else
  BASE_DOMAIN="${CERT_DOMAIN_SUFFIX}"
fi

WILDCARD_DOMAIN="*.${BASE_DOMAIN}"
CERT_BASENAME="${CERT_BASENAME:-wildcard.${BASE_DOMAIN}}"
CERT_FILE="${OUT_DIR}/${CERT_BASENAME}.crt"
KEY_FILE="${OUT_DIR}/${CERT_BASENAME}.key"

if ! command -v openssl >/dev/null 2>&1; then
  echo "openssl is required but not found in PATH" >&2
  exit 1
fi

mkdir -p "${OUT_DIR}"

openssl req \
  -x509 \
  -newkey rsa:2048 \
  -sha256 \
  -nodes \
  -days "${CERT_DAYS}" \
  -subj "/CN=${WILDCARD_DOMAIN}" \
  -addext "subjectAltName=DNS:${WILDCARD_DOMAIN},DNS:${BASE_DOMAIN}" \
  -keyout "${KEY_FILE}" \
  -out "${CERT_FILE}"

echo "base_domain: ${BASE_DOMAIN}"
echo "wildcard_domain: ${WILDCARD_DOMAIN}"
echo "certificate: ${CERT_FILE}"
echo "key: ${KEY_FILE}"
