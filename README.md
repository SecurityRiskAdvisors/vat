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
- `--target-assessment-name`: Overrides the name of the assessment being restored in the target instance.
- `--override-template-assessment`: Overrides any set template name in the serialized data and loads template test cases anyway.
- `-k`: Allow insecure connections (e.g., ignore TLS certificate errors).

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
- `-k`: Allow insecure connections (e.g., ignore TLS certificate errors).

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
cat encrypted_file | age --decrypt --passphrase | gunzip > assessment.json
```

This command will prompt for the passphrase and then extract the decrypted JSON data.

### Repackaging JSON into Encrypted Format

> **⚠️ Warning:** Manually editing assessment files can risk corrupting data structures. Proceed with caution and ensure you understand the data format before making changes.

To repackage a modified JSON file back into an encrypted archive:

```bash
cat modified_assessment.json | gzip | age --encrypt --passphrase > archive.age
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
