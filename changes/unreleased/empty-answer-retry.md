### Fixed

- When a model ended its turn without writing anything, the chat failed at once with "The model didn't write an answer". Toskar now asks the model once more, quietly, and shows that error only if it writes nothing again. It doesn't ask again after a tool has acted (#1001).
