# VECTR Assessment Transfer

This repository provides a CLI tool for saving, restoring, dumping, and transferring assessments, campaigns, and test cases from a VECTR instance. The tool interacts with the VECTR GraphQL API to manage assessment data.

> **📢 vat 2.0 is a hard breaking change.** vat 2.0 introduces a new envelope/manifest
> file format (manifest version `2`). It cannot read files saved by vat 1.x, and vat 1.x
> cannot read files saved by vat 2.0 — there is no auto-upgrade path. See
> [Upgrading from vat 1.x](#upgrading-from-vat-1x) below.

- [VECTR Assessment Transfer](#vectr-assessment-transfer)
  - [Upgrading from vat 1.x](#upgrading-from-vat-1x)
  - [How to Run](#how-to-run)
    - [Downloading the Binary](#downloading-the-binary)
    - [Supported VECTR Versions](#supported-vectr-versions)
    - [Generating VECTR Credentials](#generating-vectr-credentials)
    - [Connecting to VECTR with TLS](#connecting-to-vectr-with-tls)
      - [Using a Custom CA (`--ca-cert`)](#using-a-custom-ca---ca-cert)
      - [Insecure Connections (`--insecure` or `-k`)](#insecure-connections---insecure-or--k)
      - [Mutual TLS (mTLS)](#mutual-tls-mtls)
    - [Save Assessment Data](#save-assessment-data)
      - [Minimal Example](#minimal-example)
      - [Required Options](#required-options)
      - [Optional Options](#optional-options)
    - [Restore Assessment Data](#restore-assessment-data)
      - [Minimal Example](#minimal-example-1)
      - [Required Options](#required-options-1)
      - [Optional Options](#optional-options-1)
    - [Dump Assessment Data](#dump-assessment-data)
      - [Minimal Example](#minimal-example-2)
      - [Required Options](#required-options-2)
      - [Optional Options](#optional-options-2)
      - [Filter File Format](#filter-file-format)
    - [Transfer Assessment Data](#transfer-assessment-data)
      - [Minimal Example](#minimal-example-3)
      - [Required Options](#required-options-3)
      - [Optional Options](#optional-options-3)
    - [Restoring or Transferring a Single Campaign](#restoring-or-transferring-a-single-campaign)
      - [Example using `restore`](#example-using-restore)
    - [Recovering from a Duplicate Assessment ID](#recovering-from-a-duplicate-assessment-id)
    - [Defense Tool Reconciliation](#defense-tool-reconciliation)
    - [Force Environment Only Import](#force-environment-only-import)
    - [Diagnostic Command](#diagnostic-command)
      - [Minimal Example](#minimal-example-4)
      - [Required Options](#required-options-4)
      - [Optional Options](#optional-options-4)
    - [Debug Mode](#debug-mode)
  - [Working with Encrypted Assessment Files](#working-with-encrypted-assessment-files)
    - [Extracting JSON from Encrypted Files](#extracting-json-from-encrypted-files)
    - [Repackaging JSON into Encrypted Format](#repackaging-json-into-encrypted-format)
  - [Development](#development)
    - [Build the Application](#build-the-application)
    - [Run Tests](#run-tests)
  - [Project Structure](#project-structure)


## Upgrading from vat 1.x

vat 2.0 changed the on-disk file format (the JSON inside the encrypted archive
is now an envelope with a `manifest` and versioned `data`, instead of the flat
structure vat 1.x used — see [ARCHITECTURE.md](ARCHITECTURE.md) for details).
This is a **hard, one-way break**: vat 2.0 will refuse to decode any file saved
by vat 1.x, and vat 1.x cannot read files saved by vat 2.0. There is no
in-place conversion.

Going forward, every vat 2.x release commits to reading manifest version `2`
files, so files saved with any vat 2.0+ build will keep working across future
2.x releases — this break happens once, at the 1.x → 2.0 boundary, not again
within the 2.x line.

To move existing vat 1.x archives forward:

1. Keep (or reinstall) a vat 1.x binary, and use it to `restore` your existing
   `.vat` archives into a VECTR instance running a VECTR version compatible
   with vat 1.x.
2. Once that data lives in VECTR again, use vat 2.0 to `save`/`dump` it back
   out. The resulting files will be in the new vat 2.0 envelope format and can
   be restored with vat 2.0 going forward.

There is no supported path for vat 2.0 to read a vat 1.x archive directly —
the archive has to pass back through a live VECTR instance first.

## How to Run

After building or downloading the binary, you can use the following commands to save, restore, dump, and transfer assessment data.

### Downloading the Binary

You can download the latest binary from the [release page](https://github.com/SecurityRiskAdvisors/vat/releases). You can always find the latest release [here](https://github.com/SecurityRiskAdvisors/vat/releases/latest).

### Supported VECTR Versions

vat 1.x supports VECTR versions below 9.14; VECTR 9.14 and later require vat 2.x. Commands that connect to a VECTR instance check the live version (at major.minor granularity) and refuse to run against an unsupported one. Pass `--ignore-version-check` to downgrade that failure to a warning and proceed anyway.

### Generating VECTR Credentials

* Follow the instructions [here](https://docs.vectr.io/API-Key/) to create a VECTR API key, depending on the operation you will need specific types of access:
  * `save operations`: at least `read` from the relevant environment and the Library.
  * `restore operations`: at least `write` on the relevant environment and the Library.
* Create a credentials file:
  * Suggest using `install -m 0400 /dev/null /path/to/file`.
* Add the VECTR credentials into the file in the form of: `<key_id>:<key_secret>`.

### Connecting to VECTR with TLS

By default, `vat` attempts to establish a secure TLS connection to the VECTR instance. If the instance uses a TLS certificate that is not trusted by your system's default certificate authorities (e.g., a certificate from a private or corporate CA), you must provide a way to validate it.

#### Using a Custom CA (`--ca-cert`)

The `--ca-cert` flag is the **secure** way to connect to a VECTR instance that has a custom or internally-issued TLS certificate. You provide the public certificate of the Certificate Authority (CA) that signed the server's certificate. `vat` will use this CA to validate the server's identity, ensuring a secure and encrypted connection. This is the recommended approach for production or sensitive environments.

#### Insecure Connections (`--insecure` or `-k`)

The `--insecure` flag disables all TLS certificate validation. This means `vat` will not verify the identity of the VECTR server, making the connection vulnerable to man-in-the-middle (MITM) attacks. This option should only be used for temporary testing against development environments where you understand and accept the security risks. It is a convenient but **insecure** alternative to using `--ca-cert`.

#### Mutual TLS (mTLS)

For environments requiring client-side authentication, you can use `--client-cert-file` and `--client-key-file`. These flags provide a client certificate and private key to the VECTR server, which verifies the client's identity before allowing a connection. This is often used in addition to `--ca-cert` for a fully authenticated and encrypted channel.

### Save Assessment Data

Save assessment data from a VECTR instance to an encrypted, compressed file:

#### Minimal Example
```bash
./vat save --hostname <vectr-hostname> --env <environment-name> --assessment-name <assessment-name> --vectr-creds-file <path-to-vectr-creds-file> --output-file <path-to-output-file>
```

#### Required Options
- `--hostname`: Hostname of the VECTR instance.
- `--env`: Environment name in the VECTR instance.
- `--assessment-name`: Name of the assessment to save.
- `--vectr-creds-file`: Path to the VECTR credentials file.
- `--output-file`: Path to the output file.

#### Optional Options
- `-k`: Allow insecure connections (e.g., ignore TLS certificate errors).
- `--client-cert-file`: Path to the client certificate file for mTLS.
- `--client-key-file`: Path to the client key file for mTLS.
- `--ca-cert`: Path to a CA certificate file (can be used multiple times to add multiple CAs).

### Restore Assessment Data

Restore assessment data to a VECTR instance from an encrypted, compressed file.
This includes each campaign's timeline events, which are recreated in the
target instance alongside the assessment and test case data. If some events
fail to create, `vat` reports how many failed per campaign; use
[Debug Mode](#debug-mode) to see which events failed and why.

#### Minimal Example
```bash
./vat restore --hostname <vectr-hostname> --env <environment-name> --vectr-creds-file <path-to-vectr-creds-file> --input-file <path-to-input-file> --passphrase-file <path-to-passphrase-file>
```

#### Required Options
- `--hostname`: Hostname of the VECTR instance.
- `--env`: Environment name in the VECTR instance.
- `--vectr-creds-file`: Path to the credentials file.
- `--input-file`: Path to the encrypted input file.

#### Optional Options
- `--passphrase-file`: Path to the file containing the decryption passphrase.
- `--client-cert-file`: Path to the client certificate file for mTLS.
- `--client-key-file`: Path to the client key file for mTLS.
- `--ca-cert`: Path to a CA certificate file (can be used multiple times to add multiple CAs).
- `--target-assessment-name`: Overrides the name of the assessment being restored in the target instance. Required when using `--source-campaign-name`.
- `--source-campaign-name`: Name of a specific campaign to restore from the input file. If set, `--target-assessment-name` must be an existing assessment.
- `--override-template-assessment`: Overrides any set template name in the serialized data and loads template test cases anyway.
- `--delete-on-failure`: In the case of a failure, delete the created assessment from VECTR. (Note: this does not affect single campaign transfers)
- `--force-env-only`: Ignore any templates associated with test cases and import them as environment-only test cases. This breaks the link to the library template. (DANGEROUS)
- `--reset-id`: Mint a new globalId for the restored assessment instead of reusing the source one. Use this if VECTR rejects the restore with a duplicate globalId error (i.e. the target instance already has a copy of this assessment).
- `-k`: Allow insecure connections (e.g., ignore TLS certificate errors).
- `--client-cert-file`: Path to the client certificate file for mTLS.
- `--client-key-file`: Path to the client key file for mTLS.
- `--ca-cert`: Path to a CA certificate file (can be used multiple times to add multiple CAs).

### Dump Assessment Data

Dump all assessments from a VECTR instance:

#### Minimal Example
```bash
./vat dump --hostname <vectr-hostname> --vectr-creds-file <path-to-vectr-creds-file> --output-dir <path-to-output-directory>
```

#### Required Options
- `--hostname`: Hostname of the VECTR instance.
- `--vectr-creds-file`: Path to the VECTR credentials file.
- `--output-dir`: Directory to output the assessment files.

#### Optional Options
- `--filter-file`: Path to the filter file.
- `-k`: Allow insecure connections (e.g., ignore TLS certificate errors).
- `--client-cert-file`: Path to the client certificate file for mTLS.
- `--client-key-file`: Path to the client key file for mTLS.
- `--ca-cert`: Path to a CA certificate file (can be used multiple times to add multiple CAs).

#### Filter File Format
The filter file is a CSV file used to specify which environments and assessments should be included in the dump process. Each line should contain an environment name followed by an assessment name, separated by a comma. You can use a wildcard (`*`) to include all environments or assessments.

Example:
```
"env1","assessment1"
"env2","assessment2"
"*","assessment3"
"env3","*"
```

- The first line specifies that `assessment1` from `env1` should be dumped.
- The second line specifies that `assessment2` from `env2` should be dumped.
- The third line uses a wildcard to specify that `assessment3` should be dumped from all environments.
- The fourth line uses a wildcard to specify that all assessments from `env3` should be dumped.

### Transfer Assessment Data

Transfer an assessment from one VECTR instance directly to another:

#### Minimal Example
```bash
./vat transfer --source-hostname <source-vectr-hostname> --source-vectr-creds-file <path-to-source-credentials-file> --source-env <source-environment-name> --target-hostname <target-vectr-hostname> --target-vectr-creds-file <path-to-target-credentials-file> --target-env <target-environment-name> --assessment-name <assessment-name>
```

#### Required Options
- `--source-hostname`: Hostname of the source VECTR instance.
- `--source-vectr-creds-file`: Path to the credentials file for the source instance.
- `--source-env`: Environment name in the source VECTR instance.
- `--target-hostname`: Hostname of the target VECTR instance.
- `--target-vectr-creds-file`: Path to the credentials file for the target instance.
- `--target-env`: Environment name in the target VECTR instance.
- `--assessment-name`: Name of the assessment to transfer.

#### Optional Options
- `--target-assessment-name`: Overrides the name of the assessment in the target instance.
- `--override-template-assessment`: Overrides the template assessment set in the serialized data and uses the saved template data (lower fidelity).
- `--delete-on-failure`: In the case of a failure, delete the created assessment from VECTR. (Note: this does not affect single campaign transfers)
- `--force-env-only`: Ignore any templates associated with test cases and import them as environment-only test cases. This breaks the link to the library template. (DANGEROUS)
- `--reset-id`: Mint a new globalId for the transferred assessment instead of reusing the source one. Use this if VECTR rejects the transfer with a duplicate globalId error (i.e. the target instance already has a copy of this assessment).
- `-k`: Allow insecure connections (e.g., ignore TLS certificate errors). (will be applied for both source and dest)
- `--client-cert-file`: Path to the client certificate file for mTLS. (will be applied for both source and dest)
- `--client-key-file`: Path to the client key file for mTLS. (will be applied for both source and dest)
- `--ca-cert`: Path to a CA certificate file (can be used multiple times to add multiple CAs). (will be applied for both source and dest)
- `--target-assessment-name`: Overrides the name of the assessment in the target instance. Required when using `--source-campaign-name`.
- `--source-campaign-name`: Name of a specific campaign to transfer. If set, `--target-assessment-name` must be an existing assessment.

### Restoring or Transferring a Single Campaign

The `restore` and `transfer` commands support moving a single campaign from a source assessment into an existing target assessment. This is useful for merging campaigns or moving specific parts of an assessment without transferring the entire thing.

To do this, use the `--source-campaign-name` flag to specify which campaign to move. When using this flag, you must also provide `--target-assessment-name` with the name of an *existing* assessment on the target VECTR instance. The campaign will then be restored or transferred into that assessment.

#### Example using `restore`

First, save a full assessment that contains the campaign you want to move. Then, restore a single campaign from that file into an existing assessment:
```bash
./vat restore --hostname <target-hostname> --env <target-env> --source-campaign-name "Campaign A" --target-assessment-name "Existing Target Assessment" --input-file assessment.vat ...
```

A similar approach works for the `transfer` command.

### Recovering from a Duplicate Assessment ID

Every VECTR assessment has a `globalId`. By default, `vat` preserves the source
assessment's `globalId` when restoring or transferring, so re-running the same
`restore`/`transfer` against the same target is idempotent rather than
producing duplicates. If the target instance already has an assessment with
that `globalId` (for example, you've already restored this assessment once, or
it originated there), the operation fails with an error telling you to retry
with `--reset-id`.

Add `--reset-id` and re-run the exact same command to have `vat` mint a new
`globalId` for the assessment, landing it as an independent copy instead of
colliding with the existing one:

```bash
./vat restore --hostname <target-hostname> --env <target-env> --vectr-creds-file <path-to-vectr-creds-file> --input-file assessment.vat --reset-id ...
```

### Defense Tool Reconciliation

When restoring or transferring an assessment that references defense tools
(VECTR "BlueTools", e.g. EDR/AV products used in test case outcomes), `vat`
automatically reconciles them against the target instance instead of
requiring them to be pre-provisioned. For each defense tool, `vat` reuses a
matching tool already in the target instance (matched on name, product, and
active state), extends a close match with any missing defense layers, or
creates the product, layers, and tool from scratch if nothing matches.

A couple of things worth knowing:
- If a defense tool's data is incomplete (e.g. a blank name), `vat` fails the
  restore rather than creating a broken record in VECTR.
- If more than one matching tool already exists in the target instance, `vat`
  picks the most recently updated one and logs a warning. Use [Debug Mode](#debug-mode)
  to see which tool was chosen.

### Force Environment Only Import

The `--force-env-only` flag is an advanced option available for both `restore` and `transfer` commands. By default, `vat` attempts to preserve the link between test cases in an assessment and their corresponding templates in the VECTR library. This ensures that the restored assessment maintains its relationship with the library content.

When `--force-env-only` is used, `vat` intentionally ignores these template associations. All test cases are imported as "environment-only" test cases, meaning they exist solely within the assessment and have no link to the library.

**Use cases:**
- You want to create a snapshot of an assessment that is completely decoupled from the library.
- The source assessment uses library templates that do not exist and cannot be created in the target instance.

**Warning:** This is a destructive action regarding metadata. Once imported with this flag, the test cases cannot easily be re-linked to library templates.

### Diagnostic Command

View diagnostic information about an assessment file:

#### Minimal Example
```bash
./vat diag --input-file <path-to-input-file>
```

#### Required Options
- `--input-file`: Path to the encrypted assessment file.

#### Optional Options
- `--passphrase-file`: Path to the file containing the decryption passphrase.

This command extracts metadata from an assessment file, including VAT version information, operation dates, VECTR version, assessment name, description, and any custom metadata fields.

### Debug Mode

Enable debug mode for detailed logs:
```bash
./vat -d <command>
```

## Working with Encrypted Assessment Files

> **🔒 Security Warning:** Extracting assessment data to unencrypted JSON files will leave sensitive assessment information in plaintext on your filesystem. This data may contain confidential information about security assessments, findings, and organizational details. Always store these files securely, use appropriate file permissions, and delete them when no longer needed.

### Extracting JSON from Encrypted Files

To extract the JSON data from an encrypted assessment file in one command:

```bash
cat encrypted_file | age --decrypt | gunzip > assessment.json
```

This command will prompt for the passphrase and then extract the decrypted JSON data.

### Repackaging JSON into Encrypted Format

> **⚠️ Warning:** Manually editing assessment files can risk corrupting data structures. Proceed with caution and ensure you understand the data format before making changes.

To repackage a modified JSON file back into an encrypted archive:

```bash
cat modified_assessment.json | gzip | age --encrypt --passphrase > archive.vat
```

This command will prompt for a passphrase and create an encrypted file that can be used with the restore command.

Note: You'll need the [age encryption tool](https://github.com/FiloSottile/age) installed to perform these operations.

## Development

### Build the Application

To build the application, run:
```bash
make all
```

This will create an executable binary named `vat` in the `dist/` directory. As
part of the default build, `make all` also regenerates and validates the
GraphQL schema snapshot (`schematypes.txt`) via the `schema-snapshot` target.
The schema validator that powers this (`_buildcode/schemavalidate/`) is its
own Go module, separate from the main project's `go.mod` — if you're invoking
it directly with `go run`/`go build` instead of through `make`, pass
`-modfile=./_buildcode/schemavalidate/go.mod`.

### Run Tests

To build and run the unit tests, use:
```bash
make all test
```

## Project Structure

- **`cmd/`**: Contains CLI commands:
  - `saver.go`: Implements the `save` command for saving assessments.
  - `restorer.go`: Implements the `restore` command for restoring assessments.
  - `dumper.go`: Implements the `dump` command for dumping assessments.
  - `transfer.go`: Implements the `transfer` command for transferring assessments between instances.
  - `cmd.go`: Root command and CLI setup.
  - `version.go`: Implements the `version` command to display the application version.
  - `license.go`: Implements the `license` command to display the application license.

- **`vat/`**: Core logic for saving, restoring, and managing assessments:
  - `save.go`: Logic for saving assessment data.
  - `restore.go`: Logic for restoring assessment data.
  - `dump.go`: Logic for dumping assessment data.
  - `vat.go`: Data structures and JSON encoding/decoding.
  - `format.go`: Encodes/decodes the on-disk envelope/manifest file format (see [ARCHITECTURE.md](ARCHITECTURE.md) for details).

- **`internal/util/`**: Utility functions and client setup:
  - `client.go`: GraphQL client setup and API interactions.

- **`graphql/`**: GraphQL schema and operations.

- **`internal/dao/`**: Data access objects for interacting with the GraphQL API.

- **`_buildcode/schemavalidate/`**: Standalone Go module used by `make schema-snapshot` to generate and validate the GraphQL schema snapshot (`schematypes.txt`).
