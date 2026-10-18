### Fixed

- Sunrise and sunset questions are looked up instead of guessed (yeixio/toskar-apps#255). "What time is sunrise in Chicago this morning?" didn't count as needing current information, so a small model made a time up (6:36 AM, that day's dawn). Sunrise, sunset, dawn, dusk, first light, and golden hour now count, and the answer reads today's times for the place from the weather service, in its local time, before any search result.
