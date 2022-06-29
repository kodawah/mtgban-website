var hoverImage = document.getElementById("hoverImage");

document.addEventListener("mousemove", getMouse);

setInterval(followMouse, 10);

var mouseLoc = {x: 0, y: 0};

function getMouse(e){
    mouseLoc.x = e.pageX + 10;
    mouseLoc.y = e.pageY + 10;
}

function followMouse(){
    if (mouseLoc.x + hoverImage.width > window.innerWidth + window.pageXOffset) {
        hoverImage.style.left = (mouseLoc.x - hoverImage.width - 20) + "px";
    } else {
        hoverImage.style.left = mouseLoc.x + "px";
    }
    if (mouseLoc.y + hoverImage.height > window.innerHeight + window.pageYOffset) {
        hoverImage.style.top = (mouseLoc.y - hoverImage.height - 20) + "px";
    } else {
        hoverImage.style.top = mouseLoc.y + "px";
    }
}
