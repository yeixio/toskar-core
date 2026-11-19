### Changed

- Speaking and listening through a Reolink doorbell needs nothing more than
  reolink-cli installed on this computer. Toskar now reaches the camera with
  the connection's own address, channel and login, and runs reolink-cli's
  gateway at 127.0.0.1:9000 itself, so there's no camera to add to
  reolink-cli and no Speaker name to type. A camera you added to reolink-cli
  yourself, or a gateway elsewhere, can still be named in the connection.
