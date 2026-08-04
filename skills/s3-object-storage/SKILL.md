---
name: s3-object-storage
description: Use when S3-compatible object storage, SigV4 presigned URLs, object keys, uploads, downloads, or retention behavior change
category: best-practice
version: 0.4.0
---
# S3 Object Storage
## Authority
Quality guidance only. Security, retention, and data contracts remain authoritative in the locked design and `docs/agent-protocol.md`.
## Applicability
Apply to S3-compatible storage clients, object key design, upload/download paths, **AWS Signature Version 4 (SigV4)** presigned operations, lifecycle rules, or storage events. RustFS-compatible deployments use SigV4 as the required signing baseline.
## Required Inputs
Read data classification, tenancy boundaries, object ownership, size limits, lifecycle policy, failure/retry behavior, and the provider endpoint, region, addressing style, and required signed headers.
## Quality Criteria
Use unguessable scoped object identities, enforce server-side authorization, require SigV4 (`AWS4-HMAC-SHA256`) presigning, bind signed request fields, validate content, and make lifecycle and cleanup explicit.
## Outputs
One storage-safe implementation or scoped object-storage review conclusion.
## N/A Criteria
N/A when no object-storage boundary or object lifecycle changes.
## Stop Conditions
Stop on public exposure by default, tenant-crossing key design, uncontrolled upload, undefined deletion semantics, an unspecified signing version, or any non-SigV4 presigned URL proposal.
## Non-Goals
Do not use object existence as proof of application-level authorization.

## Operating Procedure
1. Define logical object ownership, data classification, retention/deletion, size/media constraints, server-generated key layout, and the RustFS/S3 endpoint configuration.
2. Configure the client/presigner explicitly for **SigV4**. Produce only query-signed requests whose `X-Amz-Algorithm` is `AWS4-HMAC-SHA256`; never fall back to SigV2.
3. Authorize the logical resource before signing. Bind the exact HTTP method, bucket/key, expiry, host, and every `x-amz-*`, checksum, content-type, or encryption header that the client must send.
4. Validate upload intent and completion, then persist application metadata only after the object is in the expected state. Define retries, orphan cleanup, and compensation.
5. Execute the generated URL against the target RustFS-compatible endpoint and test cross-tenant access, method/key/header mutation, expiry, cleanup, and lifecycle policy.

## Evidence Checklist
- Logical-id-to-key mapping, bucket/prefix access policy, encryption, lifecycle/retention decision, endpoint, region, and addressing style.
- SigV4 proof: `X-Amz-Algorithm=AWS4-HMAC-SHA256`; credential scope (`date/region/s3/aws4_request`); signed-header list; method; key; and expiry.
- A real RustFS/S3 compatibility test for success plus rejection when method, key, host, signed headers, query encoding, or expiry are altered.
- Authorization, presign scope/expiry, upload validation/completion, tenant isolation, and orphan/delete handling.

## Common Failure Modes
- Client-provided keys overwrite or read another tenant's object.
- A database record is committed before upload completion with no cleanup path.
- A presigned URL grants broader method, prefix, size, or lifetime than the intended operation.
- Endpoint/region/addressing-style configuration differs between signing and request execution, causing `SignatureDoesNotMatch`.
- A signed `Content-Type`, checksum, SSE, or `x-amz-*` header is omitted or changed by the browser/client after presigning.
- SigV2 is silently selected by an old SDK or compatibility setting.

## Primary Sources
- [Amazon S3 security best practices](https://docs.aws.amazon.com/AmazonS3/latest/userguide/security-best-practices.html)
- [Amazon S3 presigned URLs](https://docs.aws.amazon.com/AmazonS3/latest/userguide/using-presigned-url.html)
- [AWS SigV4 query-string authentication](https://docs.aws.amazon.com/AmazonS3/latest/developerguide/sigv4-query-string-auth.html)
- [RustFS S3 compatibility](https://docs.rustfs.com/features/s3-compatibility/)
