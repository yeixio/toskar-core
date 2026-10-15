### Added

- Manage people without a browser (#592). Through the API, Admins can:
  - add someone with an email address and a password they can sign in
    with at once, which they can be made to change first;
  - set a password for someone;
  - look up one person, or filter people by role or by disabled;
  - remove someone, deleting their chats, memories, automations, and
    files, or giving them to another person;
  - see and revoke someone's API keys and devices.

  Every change is logged in `GET /api/v1/people/activity`. Someone who
  must change their password is asked for a new one when they sign in.
