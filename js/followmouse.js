var hoverImage = document.getElementById("hoverImage");

document.addEventListener("mousemove", getMouse);

setInterval(followMouse, 10);

var mouseLoc = {x: 0, y: 0};

function getMouse(e){
    mouseLoc.x = e.pageX + 10;
    mouseLoc.y = e.pageY + 10;
}

function followMouse(){
    hoverImage.style.left = mouseLoc.x + "px";
    hoverImage.style.top = mouseLoc.y + "px";
}
