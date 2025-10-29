# VECTR Assessment Transfer

This repository provides a CLI tool for saving, restoring, dumping, and transferring assessments, campaigns, and test cases from a VECTR instance. The tool interacts with the VECTR GraphQL API to manage assessment data.

## How to Run

After building or downloading the binary, you can use the following commands to save, restore, dump, and transfer assessment data.

### Downloading the Binary

You can download the latest binary from the [release page](https://github.com/SecurityRiskAdvisors/vat/releases). You can always find the latest release [here](https://github.com/SecurityRiskAdvisors/vat/releases/latest).

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

Restore assessment data to a VECTR instance from an encrypted, compressed file:

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

This will create an executable binary named `vat` in the `dist/` directory.

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

- **`internal/util/`**: Utility functions and client setup:
  - `client.go`: GraphQL client setup and API interactions.

- **`graphql/`**: GraphQL schema and operations.

- **`internal/dao/`**: Data access objects for interacting with the GraphQL API.
