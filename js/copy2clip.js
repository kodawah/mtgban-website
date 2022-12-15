function waithide(obj) {
    obj.style.opacity = '0';
    window.setTimeout(
    function restore() {
        obj.style.opacity = '100';
    }, 150);
}
