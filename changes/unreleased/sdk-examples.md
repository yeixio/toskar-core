### Changed

- The examples use the official SDKs, in Python, JavaScript, and Go:
  streaming an answer, watching events, and connecting as a device at
  home or from anywhere. The curl examples stream through the
  OpenAI-compatible API and read `TOSKAR_URL` and `TOSKAR_API_KEY`.
- Every example runs in CI against a throwaway Core, on each pull
  request and each night, so one that breaks fails the build. The Python
  events example prints each event as it comes, even into a pipe.
- The examples install the SDKs from npm and PyPI (`toskar` 0.1.0), and
  the Go examples use the Go SDK's v0.1.0 release.
