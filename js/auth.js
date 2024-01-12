const auth = firebase.auth();
const db = firebase.firestore();

const signupForm = document.getElementById('signup-form');
const signinForm = document.getElementById('signin-form');

// Listen for sign-up form submission events
signupForm.addEventListener('submit', (e) => {
    e.preventDefault();
    const email = signupForm['email'].value;
    const password = signupForm['password'].value;

    // Create a new user with the provided email and password
    auth.createUserWithEmailAndPassword(email, password)
        .then(userCredential => {
            // Save the user's email and password in Firestore
            return db.collection('users').doc(userCredential.user.uid).set({
                email: userCredential.user.email,
            });
        })
        .then(() => {
            console.log('User signed up and data stored.');
            signupForm.reset();
        })
        .catch(error => {
            console.error('Error signing up:', error.message);
        });
});

// listen for login form submission events
signinForm.addEventListener('submit', (e) => {
    e.preventDefault();
    const email = signupForm['email'].value;
    const password = signupForm['password'].value;

    auth.signInWithEmailAndPassword(email, password)
        .then(userCredential => {
            return userCredential.user.getIdToken();
        })
        .then(idToken => {
            fetch('backend/endpoint - TODO', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'Authorization': idToken,
                },
            })
                .then(response => response.text())
                .then(data => {
                    console.log('Response from backend:', data);
                })
                .catch(error => {
                    console.error('Error communicating with backend:', error);
                });

            signinForm.reset();
        })
        .catch(error => {
            console.error("Error signing in:", error.message);
        });
});

async function getUserData(uid) {
    try {
        const docRef = db.collection('users').doc(uid);
        const doc = await docRef.get();
        if (doc.exists) {
            return doc.data();
        } else {
            console.log('no user data found');
        }
    } catch (error) {
        console.error('Error retrieving user data:', error);
    }
}
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