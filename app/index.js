const errorAlert = document.getElementById("error-alert");
const encryptForm = document.querySelector("#encrypt-tab form");
const decryptForm = document.querySelector("#decrypt-tab form");
const encryptResultDiv = document.getElementById("encrypt-result");
const decryptResultDiv = document.getElementById("decrypt-result");
const decryptKey = document.getElementById("decrypt-key");

function createDecryptLink(pwd, key) {
  return `${window.location.origin}${window.location.pathname}#${pwd}-${key}`;
}

function parseDecryptKey() {
  const [pwd, key] = decryptKey.value.split("-");
  return { pwd, key };
}

function setDecryptKeyFromLocation() {
  const hash = window.location.hash;
  if (!!hash) {
    decryptKey.value = hash.substring(1);
    return true;
  }
  return false;
}

function encodeBase64(bytes) {
  const numbers = new Uint8Array(bytes);
  return btoa(String.fromCharCode(...numbers));
}

function decodeBase64(b64Text) {
  const binaryString = atob(b64Text);
  const bytes = new Uint8Array(binaryString.length);
  for (let i = 0; i < binaryString.length; i++) {
    bytes[i] = binaryString.charCodeAt(i);
  }
  return bytes;
}

function createPassword() {
  return window.crypto.randomUUID().replaceAll("-", "");
}

async function pwdToKey(pwd) {
  const encPwd = new TextEncoder().encode(pwd);
  const rawKey = await window.crypto.subtle.digest("SHA-256", encPwd);
  return window.crypto.subtle.importKey("raw", rawKey, "AES-GCM", false, ["encrypt", "decrypt"]);
}

async function encryptSecret(pwd, plainText) {
  const key = await pwdToKey(pwd);

  const secret = new TextEncoder().encode(plainText);
  const ivBytes = window.crypto.getRandomValues(new Uint8Array(12));
  const encBytes = await window.crypto.subtle.encrypt({ name: "AES-GCM", iv: ivBytes }, key, secret);

  const ivText = encodeBase64(ivBytes);
  const encText = encodeBase64(encBytes);
  return `${ivText}~${encText}`;
}

async function decryptSecret(pwd, cipherText) {
  const key = await pwdToKey(pwd);

  const [ivText, encText] = cipherText.split("~");
  const ivBytes = decodeBase64(ivText);
  const encBytes = decodeBase64(encText);

  const secret = await window.crypto.subtle.decrypt({ name: "AES-GCM", iv: ivBytes }, key, encBytes);
  return new TextDecoder().decode(secret);
}

function handleFetchResponse(res) {
  if (res.ok) {
    return res.text();
  }
  if (res.headers.get("Content-Type").startsWith("text/plain")) {
    return res.text().then((txt) => {
      throw new Error(`${res.status}: ${txt}`);
    });
  }
  throw new Error(`${res.status}: ${res.statusText}`);
}

function setSecret(secret, ttl) {
  const body = new URLSearchParams();
  body.set("secret", secret);
  body.set("ttl", ttl);
  const opts = {
    method: "POST",
    body: body,
  };
  return fetch("/push", opts).then(handleFetchResponse);
}

function getSecret(secretKey) {
  const body = new URLSearchParams();
  body.set("key", secretKey);
  const opts = {
    method: "POST",
    body: body,
  };
  return fetch("/pull", opts).then(handleFetchResponse);
}

function showElement(element) {
  element.style.display = "";
}

function hideElement(element) {
  element.style.display = "none";
}

function disableForm(form) {
  form.querySelector("fieldset").disabled = true;
}

function enableForm(form) {
  form.querySelector("fieldset").disabled = false;
}

function updateErrorAlert(message) {
  errorAlert.textContent = message;
  showElement(errorAlert);
}

function updateEncryptResults(pwd, key, ttl) {
  const link = createDecryptLink(pwd, key);
  const ttlTxt = ttl === "1" ? "1 hour" : `${ttl} hours`;
  const expiry = Date.now() + parseInt(ttl) * 60 * 60 * 1000;
  const expiryTxt = new Date(expiry).toLocaleString();

  encryptResultDiv.querySelector(".copy-me").textContent = link;
  encryptResultDiv.querySelector(".expire-in").textContent = ttlTxt;
  encryptResultDiv.querySelector(".expire-at").textContent = expiryTxt;
  showElement(encryptResultDiv);
}

function updateDecryptResults(secret) {
  decryptResultDiv.querySelector(".copy-me").textContent = secret;
  showElement(decryptResultDiv);
}

decryptKey.addEventListener("input", () => {
  if (decryptKey.validity.patternMismatch) {
    decryptKey.setCustomValidity("Invalid shared key.");
  } else {
    decryptKey.setCustomValidity("");
  }
});

encryptForm.addEventListener("submit", (evt) => {
  evt.preventDefault();

  const secret = document.getElementById("encrypt-value").value;
  const ttl = document.getElementById("encrypt-ttl").value;
  const pwd = createPassword();

  hideElement(errorAlert);
  hideElement(encryptResultDiv);
  disableForm(encryptForm);

  encryptSecret(pwd, secret)
    .then((cipherText) => {
      return setSecret(cipherText, ttl);
    })
    .then((secretKey) => {
      updateEncryptResults(pwd, secretKey, ttl);
      enableForm(encryptForm);
    })
    .catch((ex) => {
      console.error(ex);
      updateErrorAlert(ex.toString());
      enableForm(encryptForm);
    });
});

decryptForm.addEventListener("submit", (evt) => {
  evt.preventDefault();

  const shared = parseDecryptKey();

  hideElement(errorAlert);
  hideElement(decryptResultDiv);
  disableForm(decryptForm);

  getSecret(shared.key)
    .then((cipherText) => {
      return decryptSecret(shared.pwd, cipherText);
    })
    .then((secret) => {
      updateDecryptResults(secret);
      enableForm(decryptForm);
    })
    .catch((ex) => {
      console.error(ex);
      updateErrorAlert(ex.toString());
      enableForm(decryptForm);
    });
});

document.querySelectorAll(".initially-hidden").forEach((elt) => {
  hideElement(elt); // take over hiding from css
  elt.classList.remove("initially-hidden");
});

document.querySelectorAll("div.card pre").forEach((pre) => {
  pre.addEventListener("click", () => {
    navigator.clipboard.writeText(pre.textContent).then(() => {
      pre.classList.add("is-copied");
      window.setTimeout(function () {
        pre.classList.remove("is-copied");
      }, 1000);
    });
  });
});

document.querySelectorAll("#tab-row button").forEach((btn) => {
  btn.addEventListener("shown.bs.tab", () => {
    const selector = `${btn.dataset.bsTarget} .focus-target`;
    document.querySelector(selector).focus();
  });
});

if (setDecryptKeyFromLocation()) {
  document.querySelectorAll("#encrypt-tab-btn, #encrypt-tab").forEach((elt) => {
    elt.classList.remove("active");
  });
  document.querySelectorAll("#decrypt-tab-btn, #decrypt-tab").forEach((elt) => {
    elt.classList.add("active");
  });
  document.querySelector("#decrypt-tab .focus-target").focus();
} else {
  document.querySelector("#encrypt-tab .focus-target").focus();
}
