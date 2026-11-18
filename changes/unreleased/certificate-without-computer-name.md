### Fixed

- The certificate a computer shows anyone who connects, from the internet
  or through the relay, no longer has its name. Certificates made before
  carry it, so Toskar makes a new one and switches once the paired phones
  have learned it, or after 30 days. Phones with an app from before see
  the usual "certificate changed" check once (#1263).
