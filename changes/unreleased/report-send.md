### Added

- `POST /api/v1/diagnostics/report` sends a problem report to Toskar's report service, which files it as an issue grouped with others for the same error and version, instead of the person writing an email. It's listed in What left this computer, an Admin can turn it off with the `problem_reports` setting, and paired devices may send through it too (#588, contract 1.40).
