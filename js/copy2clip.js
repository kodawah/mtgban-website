function copyAndBlink(obj, str) {
    navigator.clipboard.writeText(str);
    obj.style.opacity = '0';
    window.setTimeout(
    function restore() {
        obj.style.opacity = '100';
    }, 150);
}
