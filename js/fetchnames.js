const url = "https://api.scryfall.com/catalog/card-names";

/*
 * Query Scryfall to retrieve the list of card names.
 */
async function fetchNames() {
    let cardNames = await fetch(url)
        // Transform the data into json
        .then(response => response.json())
        // Return the array present in .data
        .then(scryfallOutput => scryfallOutput.data);
    return cardNames;
}
