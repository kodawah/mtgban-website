const scryfallAPI = require('./scryfall.js');
let suggestions = {};

class Suggestion {
    constructor() {
        this.color = [
            'white', 'blue', 'black', 'red', 'green', 'colorless',
            'azorius', 'gruul', 'dimir', 'orzhov', 'izzet',
            'rakdos', 'golgari', 'simic', 'selesnya', 'boros',
            'bant', 'esper', 'jund', 'grixis', 'naya',
            'abzan', 'jeskai', 'sultai', 'mardu', 'temur',
            'quandrix', 'witherbloom', 'lorehold', 'silverquill', 'prismari',
            'ink', 'glint', 'dune', 'witch', 'yore',
            'chaos', 'aggression', 'altruism', 'growth', 'artifice',
            'wubrg', 'rainbow'
        ];

        this.condition = [
            'NM', 'SP','LP','MP','HP','PO','DMG'
        ];

        this.finish = [
            'foil','nonfoil','etched'
        ];

        this.rarity = [
            'mythic','rare','uncommon','common','special','token','oversize'
        ];

        this.property = [
            'reserved', 'token', 'oversize', 'funny', 'wcd', 'commander', 'sldpromo'
        ];

        this.lang = [
            'jp', 'jpn', 'ph', 'phrexian'
        ];

        this.frame = [
            'fullart', 'fa', 'extendedart', 'ea',
            'showcase', 'sc', 'borderless', 'bd',
            'reskin', 'gold', 'retro'
        ];

        this.promo = [
            'arenaleague', 'boosterfun', 'bundle', 'buyabox', 'concept', 'confettifoil',
            'doublerainbow', 'draculaseries', 'draftweekend', 'embossed', 'galaxyfoil',
            'gameday', 'gilded', 'glossy', 'godzillaseries', 'halofoil', 'intropack', 'promo',
            'judgegift', 'neonink', 'oilslick', 'playpromo', 'playerrewards', 'poster',
            'prerelease', 'promopack', 'release', 'schinesealtart', 'scroll', 'serialized',
            'silverfoil', 'starterdeck', 'stepandcompleat', 'surgefoil', 'textured', 'thick',
            'wizardsplaynetwork',
        ];

        this.type = [
            async () => {
                const types = await CacheManager.serveData(this.fetchTypes.bind(this)),
                    type = types['Creature'].concat(types['Planeswalker'], types['Land'], types['Artifact'], types['Enchantment'], types['Spells']);
                return type
            }
        ];

        this.set = [
            async () => {
                const sets = await CacheManager.serveData(this.fetchSets.bind(this)),
                    set = sets['sets'].map(set => set.code);
                return set
            }
        ];
        this.name = [
            async () => {
                const names = await CacheManager.serveData(this.fetchNames.bind(this)),
                    name = names['names'];
                return name
            }
        ];
    }  
};



function debounce(func, delay) {
    let debounceTimer;
    return function () {
        const context = this;
        const args = arguments;
        clearTimeout(debounceTimer);
        debounceTimer = setTimeout(() => func.apply(context, args), delay);
    };
};

function displaySuggestions(suggestions, input, prefix) {
    suggestions.forEach(suggestion => {
        const suggestionElement = document.createElement('DIV');
        suggestionElement.innerHTML = `<strong>${prefix}:</strong> ${suggestion}`;
        suggestionElement.addEventListener('click', function (e) {
            input.value = `${prefix}:${suggestion}`;
            closeAllLists();
        });
        input.parentNode.appendChild(suggestionElement);
    });
};

function closeAllLists() {
    const suggestions = document.getElementsByClassName('asuggestion-items');
    for (let i = 0; i < suggestions.length; i++) {
        suggestions[i].parentNode.removeChild(suggestions[i]);
    }
};


function addActive(suggestions) {
    if (!suggestions) {
        return false;
    }
    removeActive(suggestions);
    if (currentFocus >= suggestions.length) {
        currentFocus = 0;
    }
    if (currentFocus < 0) {
        currentFocus = (suggestions.length - 1);
    }
    suggestions[currentFocus].classList.add('suggestion-active');
};


function removeActive(suggestions) {
    for (let i = 0; i < suggestions.length; i++) {
        suggestions[i].classList.remove('suggestion-active');
    }
};


async function getSuggestionsByPrefix(prefix, Input) {
    switch (prefix) {
        case 's':
            return filterList(await scryfallAPI.getSetsData(), Input, 'code');
        case 't':
            return filterList(await scryfallAPI.getTypesData(), Input);
        case 'r':
            return filterList(rarities, Input);
        case 'c':
            return filterList(colors, Input);
        case 'cond':
            return filterList(conditions, Input);
        case 'f':
            return filterList(finishes, Input);
        default:
            return [];
    }
}

function filterList(list, input, key) {
    return list.filter(item => {
        if (key) {
            return item[key].toLowerCase().includes(input.toLowerCase());
        }
        return item.toLowerCase().includes(input.toLowerCase());
    });
}

async function autocomplete(form, input) {
    var currentFocus;
    var minlen = 3;
    const arr = Suggestion[names];

    /* Execute a function when someone writes in the text field: */
    input.addEventListener("input", function (e) {
        var a, b, i, val = this.value;
        /* Close any already open lists of autocompleted values */
        closeAllLists();
        if (!val) {
            return false;
        }
        /* Prompt suggestions only if input is longer than three characters */
        if (val.length < minlen) {
            return false;
        }
        currentFocus = -1;
        /* Create a DIV element that will contain the items (values) */
        a = document.createElement("DIV");
        a.setAttribute("id", this.id + "autocomplete-list");
        a.setAttribute("class", "autocomplete-items");

        /* Append the DIV element as a child of the autocomplete container */
        this.parentNode.appendChild(a);

        /* For each item in the array... */
        for (i = 0; i < arr.length; i++) {
            /* Check if the item starts with the same letters as the text field value */
            if (arr[i].substr(0, val.length).toUpperCase() == val.toUpperCase() || arr[i].substr(0, val.length).normalize("NFD").replace(/[\u0300-\u036f]/g, "").toUpperCase() == val.toUpperCase()) {
                /* Create a DIV element for each matching element */
                b = document.createElement("DIV");

                /* Make the matching letters bold */
                b.innerHTML = "<strong>" + arr[i].substr(0, val.length) + "</strong>";
                b.innerHTML += arr[i].substr(val.length);

                /* Insert a input field that will hold the current array item's value */
                b.innerHTML += "<input type='hidden' value='" + arr[i].replace("'", "&apos;").replace("\"", "&quot;") + "'>";
                /* Execute a function when someone clicks on the item value (DIV element) */
                b.addEventListener("click", function (e) {
                    /* Insert the value for the autocomplete text field */
                    inp.value = this.getElementsByTagName("input")[0].value;
                    /* Close the list of autocompleted values,
                     * (or any other open lists of autocompleted values */
                    closeAllLists();

                    /* Submit the form (so that onSubmit may trigger) */
                    /* We need to use this extended workaround due to Safari */
                    const fakeButton = document.createElement('button');
                    fakeButton.type = this.type;
                    fakeButton.style.display = 'none';
                    form.appendChild(fakeButton);
                    fakeButton.click();
                    fakeButton.remove();
                });
                a.appendChild(b);
            }
        }
    });

    /* Execute a function presses a key on the keyboard */
    inp.addEventListener("keydown", function (e) {
        var x = document.getElementById(this.id + "autocomplete-list");
        if (x) {
            x = x.getElementsByTagName("div");
        }
        if (e.keyCode == 40) { // DOWN key
            /* If the arrow DOWN key is pressed,
             * do not move input cursor */
            e.preventDefault();
            if (!x || x.length == 0) {
                /* ignore the minimum input length */
                minlen = 1;
                /* force the drop-down menu to appear */
                this.dispatchEvent(new InputEvent("input", e));
            } else {
                /* increase the currentFocus variable */
                currentFocus++;
                /* prevent overflowing */
                if (x && currentFocus > x.length - 1) {
                    currentFocus = 0;
                }
                /* and and make the current item more visible */
                addActive(x);
            }
        } else if (e.keyCode == 38) { // UP key
            /* If the arrow UP key is pressed,
             * do not move input cursor */
            e.preventDefault();
            /* decrease the currentFocus variable */
            currentFocus--;
            /* prevent overflowing */
            if (currentFocus < 0) {
                currentFocus = x ? x.length - 1 : 0;
            }
            /* and and make the current item more visible */
            addActive(x);
        } else if (e.keyCode == 13 && currentFocus > -1) {
            /* If the ENTER key is pressed and if the selector is open */
            if (x) {
                /* simulate a click on the "active" item */
                x[currentFocus].click();
            }
        } else if (e.keyCode == 27) {
            /* If the ESC key is pressed just close everything */
            closeAllLists();
        } else if ((e.keyCode == 9 || e.keyCode == 39) && currentFocus > -1) {
            /* If the TAB or RIGHT ARROW keys are pressed, and if the selector
             * is open, do not move focus */
            e.preventDefault();
            /* initialize the input field with what is selected */
            this.value = x[currentFocus].textContent;
        }
    });

    /* Classify an item as "active" */
    function addActive(x) {
        if (!x) {
            return false;
        }
        /* Start by removing the "active" class on all items */
        removeActive(x);
        if (currentFocus >= x.length) {
            currentFocus = 0;
        }
        if (currentFocus < 0) {
            currentFocus = (x.length - 1);
        }
        /* Add class "autocomplete-active" */
        x[currentFocus].classList.add("autocomplete-active");
    }

    /* Remove the "active" class from all autocomplete items */
    function removeActive(x) {
        for (var i = 0; i < x.length; i++) {
            x[i].classList.remove("autocomplete-active");
        }
    }

    /* Close all autocomplete lists in the document, except the one passed as an argument */
    function closeAllLists(elmnt) {
        var x = document.getElementsByClassName("autocomplete-items");
        for (var i = 0; i < x.length; i++) {
            if (elmnt != x[i] && elmnt != inp) {
                x[i].parentNode.removeChild(x[i]);
            }
        }
    }

    /* Execute a function (make the suggestions disaeppear)
     * when someone clicks in the document */
    document.addEventListener("click", function (e) {
        closeAllLists(e.target);
    });
};
