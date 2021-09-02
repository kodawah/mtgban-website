let theme = localStorage.getItem("theme");
// If the current theme in localStorage is "dark"...
if (theme == "dark") {
    // ...then use the .dark-theme class
    document.body.classList.add("dark-theme");
}
