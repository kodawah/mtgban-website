const auth = firebase.auth();
const db = firebase.firestore();
const signupForm = document.getElementById('signup-form');
const signinForm = document.getElementById('signin-form');

// Listen for sign-up form submission events
signupForm.addEventListener('submit', (e) => {
    e.preventDefault();
    const email = e.target.elements.email.value;
    const password = e.target.elements.password.value;
    // Create a new user with the provided email and password
    auth.createUserWithEmailAndPassword(email, password)
        .then(userCredential => {
            // Save the user's email and password in Firestore
            const user = userCredential.user;
            return db.collection('users').doc(user.uid).set({
                email: user.email,
                password: user.password
            });
        })
        .then(() => {
            console.log('User signed up and data stored.');
        })
        .catch(error => {
            console.error('Error signing up:', error);
        });
});
// listen for login form submission events
signinForm.addEventListener('submit', (e) => {
    e.preventDefault();
    const email = e.target.elements.email.value;
    const password = e.target.elements.password.value;
    // Authenticate the user
    auth.signInWithEmailAndPassword(email, password)
        .then(userCredential => {
            // User is now signed in.
            const user = userCredential.user;
            const token = user.getIdToken();
        })
        .catch(error => {
            console.error("Error signing in:", error);
        });
});
/**
 *  by setting a reference field in the users document 
 *  we can easily access associated document values, like the ACL...
 *  or traditional 'cookies' data - but we can persist it
 *  in firebase to the benefit of our users
 */
async function getCookie(uid, key) {
    try {
        const docRef = db.collection('users').doc(uid);
        const doc = await docRef.get();
        if (doc.exists) {
            return doc.data()[key];
        } else {
            console.log('No such document!');
        }
    } catch (error) {
        console.error('Error getting cookie: ', error);
    }
}

async function setCookie(uid, key, value) {
    try {
        const docRef = db.collection('users').doc(uid);
        await docRef.update({
            [key]: value,
        });
    } catch (error) {
        console.error('Error updating cookie: ', error);
    }
}