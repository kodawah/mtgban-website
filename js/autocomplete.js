/*
 * The autocomplete function takes a form containing an input field.
 * It will load the names to be completed once and create div elemenents
 * containing possible suggestions.
 * If a user scrolls up and down, selects an entry and presses Enter, or
 * clicks on a field, they will be submitting the form automatically.
 */
async function autocomplete(form, inp) {
    var currentFocus;
    var minlen = 3;
    const arr = await fetchNames();

    /* Execute a function when someone writes in the text field: */
    inp.addEventListener("input", function(e) {
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
            if (arr[i].substr(0, val.length).toUpperCase() == val.toUpperCase()) {
                /* Create a DIV element for each matching element */
                b = document.createElement("DIV");

                /* Make the matching letters bold */
                b.innerHTML = "<strong>" + arr[i].substr(0, val.length) + "</strong>";
                b.innerHTML += arr[i].substr(val.length);

                /* Escape single quotes from autocompleted names */
                if (arr[i].includes("'")){
                    arr[i] = arr[i].replace("'", "&apos;")
                }
                /* Insert a input field that will hold the current array item's value */
                b.innerHTML += "<input type='hidden' value='" + arr[i] + "'>";
                /* Execute a function when someone clicks on the item value (DIV element) */
                b.addEventListener("click", function(e) {
                    /* Insert the value for the autocomplete text field */
                    inp.value = this.getElementsByTagName("input")[0].value;
                    /* Close the list of autocompleted values,
                     * (or any other open lists of autocompleted values */
                    closeAllLists();

                    /* Submit the form */
                    form.submit();
                });
                a.appendChild(b);
            }
        }
    });

    /* Execute a function presses a key on the keyboard */
    inp.addEventListener("keydown", function(e) {
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
            if (currentFocus < 0 ) {
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
    document.addEventListener("click", function(e) {
        closeAllLists(e.target);
    });
};
