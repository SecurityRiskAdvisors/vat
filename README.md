# VECTR Assessment Transfer

This repository provides a CLI tool for saving and restoring assessments, campaigns, and test cases from a VECTR instance. The tool interacts with the VECTR GraphQL API to manage assessment data.

## How to Run

After building or downloading the binary, you can use the following commands to save and restore assessment data.

### Generating VECTR credentials

* Follow the instructions [here](https://docs.vectr.io/API-Key/) to create a VECTR API key, depending on the operation you will need specific types of access
  * `save operations`:  at least `read` from the relevant DB and the Library
  * `restore operations`:  at least `write` on the relevant DB and the Library
* Create a credentials file
  * Suggest using `install -m 0400 /dev/null /path/to/file`
* Add the VECTR credentials into the file in the form of: `<key_id>:<key_secret>`

### Save Assessment Data

Save assessment data from a VECTR instance to an encrypted, compressed file:
```bash
./vat save \
  --hostname <vectr-hostname> \
  --db <database-name> \
  --assessment-name <assessment-name> \
  --vectr-creds-file <path-to-vectr-creds-file> \
  --output-file <path-to-output-file>
```

### Restore Assessment Data

Restore assessment data to a VECTR instance from an encrypted, compressed file:
```bash
./vat restore \
  --hostname <vectr-hostname> \
  --db <database-name> \
  --vectr-creds-file <path-to-vectr-creds-file> \
  --input-file <path-to-input-file> \
  --passphrase-file <path-to-passphrase-file> \
  --target-assessment-name <new-assessment-name>
```

- `--target-assessment-name`: (Optional) Overrides the name of the assessment being restored in the target instance.
- `--ignore-template`: (Optional) Overrides the template assessment set in the serialized data and uses the saved template data (lower fidelity)
### Directly Transfer Assessment Data

Transfer an assessment from one VECTR instance directly to another:
```bash
 ./vat transfer \
   --source-hostname <source-vectr-hostname> \
   --source-vectr-creds-file <path-to-source-credentials-file> \
   --source-db <source-database-name> \
   --target-hostname <target-vectr-hostname> \
   --target-vectr-creds-file <path-to-target-credentials-file> \
   --target-db <target-database-name> \
   --assessment-name <assessment-name> \
   --target-assessment-name <new-assessment-name>
```

- `--target-assessment-name`: (Optional) Overrides the name of the assessment in the target instance.
- `--ignore-template`: (Optional) Overrides the template assessment set in the serialized data and uses the saved template data (lower fidelity)



### Debug Mode

Enable debug mode for detailed logs:
```bash
./vat --debug <command>
```

### Flags

#### Common Flags
- `--hostname`: Hostname of the VECTR instance (required for `save` and `restore`).
- `--db`: Database name in the VECTR instance (required for `save` and `restore`).
- `--vectr-creds-file`: Path to the file containing the API key (required for all commands).
- `--insecure`: Allow insecure connections (e.g., ignore TLS certificate errors).
- `--assessment-name`: Name of the assessment to save, restore, or transfer (required for all commands).
- `--ignore-template`: (Optional) Overrides the template assessment set in the serialized data and uses the saved template data (lower fidelity)

#### Transfer-Specific Flags
- `--source-hostname`: Hostname of the source VECTR instance (required for `transfer`).
- `--source-vectr-creds-file`: Path to the credentials file for the source instance (required for `transfer`).
- `--source-db`: Database name in the source VECTR instance (required for `transfer`).
- `--target-hostname`: Hostname of the target VECTR instance (required for `transfer`).
- `--target-vectr-creds-file`: Path to the credentials file for the target instance (required for `transfer`).
- `--target-db`: Database name in the target VECTR instance (required for `transfer`).

## Development

### Build the Application

To build the application, run:
```bash
make all
```

This will create an executable binary named `vat` in the `dist/` directory.

### Run Tests

To run the unit tests, use:
```bash
make test
```

## File Structure

- **`cmd/`**: Contains CLI commands:
  - `saver.go`: Implements the `save` command.
  - `restorer.go`: Implements the `restore` command, including support for overriding assessment names.
  - `cmd.go`: Root command and CLI setup.
  - `transfer.go`: Implements the `transfer` command, allowing for direct transfer of assessments between instances with optional name overrides.
- **`vat/`**: Core logic for saving and restoring assessments:
  - `save.go`: Logic for saving assessment data.
  - `restore.go`: Logic for restoring assessment data.
  - `client.go`: GraphQL client setup and API interactions.
  - `vat.go`: Data structures and JSON encoding/decoding.
- **`graphql/`**: GraphQL schema and operations.
