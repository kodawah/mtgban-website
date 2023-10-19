function copyAndBlink(obj, str) {
    str = str.split(' // ')[0];
    navigator.clipboard.writeText(str);
    obj.style.opacity = '0';
    window.setTimeout(
    function restore() {
        obj.style.opacity = '100';
    }, 150);
}
