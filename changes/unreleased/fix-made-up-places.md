### Fixed

- Chat no longer makes up restaurants and other businesses for questions
  such as "What restaurants are in town?". Toskar now looks these up first,
  searching in the location from Personal settings, tells the model to name
  only places the search found, and says it couldn't look them up when web
  search is off rather than list places from memory. Every answer is also
  told not to invent names, addresses, phone numbers, hours, prices, quotes,
  statistics, or citations.
