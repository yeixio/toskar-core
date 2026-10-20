### Added

- An automation can run the moment a device changes, such as a Home Assistant doorbell, motion sensor, or door contact: a device trigger, with the state to wait for and a cooldown so it runs once per event. Set it with `{"kind": "device", …}` in the API or `toskarctl automations … --trigger device --device binary_sensor.front_door_visitor --to on`.
