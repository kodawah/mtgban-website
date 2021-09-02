// Select the theme preference from localStorage
const themeSwitch = document.querySelector('input');
const themeTitle = document.querySelector('span[class="slider"]');

// If the current theme in localStorage is "dark"...
if (localStorage.getItem("theme") == "dark") {
    themeSwitch.checked = true;
    themeTitle.title = "Nightbound"
} else {
    themeTitle.title = "Daybound"
}

themeSwitch.addEventListener('change', () => {
    document.body.classList.toggle('dark-theme');

    let theme = "light";

    // If the body contains the .dark-theme class...
    if (document.body.classList.contains("dark-theme")) {
        // ...then let's make the theme dark
        theme = "dark";
        themeTitle.title = "Nightbound"
    } else {
        themeTitle.title = "Daybound"
    }

    // Then save the choice in localStorage
    localStorage.setItem("theme", theme);
});
