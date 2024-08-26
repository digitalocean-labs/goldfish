// allow jquery to take over elements hidden by css during page load
$(".initially-hidden").hide().removeClass("initially-hidden");

let showDecryptTab = false;
// using hash to prevent key & pwd from being sent to backend
const query = window.location.hash;
if (!!query) {
  const params = new URLSearchParams(query.substring(1));
  const key = params.get("key");
  const pwd = params.get("pwd");
  if (!!key && !!pwd) {
    showDecryptTab = true;
    $("#decrypt-key").val(key);
    $("#decrypt-pwd").val(pwd);
  }
}
if (showDecryptTab) {
  $("#encrypt-tab-btn, #encrypt-tab").removeClass("active");
  $("#decrypt-tab-btn, #decrypt-tab").addClass("active");
  $("#decrypt-tab .focus-target").trigger("focus");
} else {
  $("#encrypt-tab .focus-target").trigger("focus");
}

$("#tab-row button").on("shown.bs.tab", function (evt) {
  const tabBtn = $(evt.target);
  $(tabBtn.data("bs-target"))
    .find(".focus-target")
    .trigger("focus");
});

$("#random-pwd-btn").on("click", function (evt) {
  evt.preventDefault();
  const alphabet = "abcdefghijklmnopqrstuvwxyz23456789";
  const values = window.crypto.getRandomValues(new Uint32Array(42)).map(val => val % alphabet.length);
  const pwd = [];
  for (const value of values) {
    pwd.push(alphabet.charAt(value));
  }
  $("#encrypt-pwd").val(pwd.join(""));
});

$("#copy-pwd-btn").on("click", function (evt) {
  evt.preventDefault();
  const self = $(this);
  const text = self.text();
  const pwd = $("#encrypt-pwd").val();
  if (!!pwd) {
    navigator.clipboard.writeText(pwd).then(() => {
      self.text("Copied");
      window.setTimeout(function () {
        self.text(text);
      }, 1000);
    });
  }
});

$("div.card pre").on("click", function () {
  const self = $(this);
  navigator.clipboard.writeText(self.text()).then(() => {
    self.addClass("is-copied");
    window.setTimeout(function () {
      self.removeClass("is-copied");
    }, 1000);
  });
});

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

async function pwdToKey(pwd) {
  const encPwd = new TextEncoder().encode(pwd);
  const rawKey = await window.crypto.subtle.digest("SHA-256", encPwd);
  return window.crypto.subtle.importKey("raw", rawKey, "AES-GCM", false, ["encrypt", "decrypt"]);
}

async function encryptSecret(pwd, plainText) {
  const key = await pwdToKey(pwd)

  const secret = new TextEncoder().encode(plainText);
  const ivBytes = window.crypto.getRandomValues(new Uint8Array(12));
  const encBytes = await window.crypto.subtle.encrypt({ name: "AES-GCM", iv: ivBytes }, key, secret);

  const ivText = encodeBase64(ivBytes);
  const encText = encodeBase64(encBytes);
  return `${ivText}~${encText}`;
}

async function decryptSecret(pwd, cipherText) {
  const key = await pwdToKey(pwd)

  const [ivText, encText] = cipherText.split('~');
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
  return fetch("/secret", opts).then(handleFetchResponse);
}

function getSecret(secretKey) {
  const params = new URLSearchParams();
  params.set("key", secretKey);
  return fetch(`/secret?${params}`).then(handleFetchResponse);
}

function disableForm(form) {
  form.find("fieldset").prop("disabled", true);
}

function enableForm(form) {
  form.find("fieldset").prop("disabled", false);
}

const errorAlert = $("#error-alert");
const encryptResultDiv = $("#encrypt-result");
const decryptResultDiv = $("#decrypt-result");

function updateEncryptResults(pwd, key, ttl) {
  const ttlTxt = (ttl === "1") ? "1 hour" : `${ttl} hours`;
  const expiry = Date.now() + (parseInt(ttl) * 60 * 60 * 1000);
  const expiryTxt = new Date(expiry).toLocaleString();

  const params = new URLSearchParams();
  params.set("key", key);
  params.set("pwd", pwd);

  // using hash to prevent key & pwd from being sent to backend
  const link = `${window.location.origin}${window.location.pathname}#${params}`;

  encryptResultDiv.find(".result-key").text(key);
  encryptResultDiv.find(".result-link").text(link);
  encryptResultDiv.find(".expire-in").text(ttlTxt);
  encryptResultDiv.find(".expire-at").text(expiryTxt);
  encryptResultDiv.show();
}

function updateDecryptResults(secret) {
  decryptResultDiv.find("pre").text(secret);
  decryptResultDiv.show();
}

$("#encrypt-tab form").on("submit", function (evt) {
  evt.preventDefault();

  const self = $(this);
  disableForm(self);

  errorAlert.hide();
  encryptResultDiv.hide();

  const secret = $("#encrypt-value").val();
  const pwd = $("#encrypt-pwd").val();
  const ttl = $("#encrypt-ttl").val();

  encryptSecret(pwd, secret)
    .then((cipherText) => {
      return setSecret(cipherText, ttl);
    })
    .then((secretKey) => {
      updateEncryptResults(pwd, secretKey, ttl);
      enableForm(self);
    })
    .catch((ex) => {
      console.error(ex);
      errorAlert.text(ex.toString()).show();
      enableForm(self);
    });
});

$("#decrypt-tab form").on("submit", function (evt) {
  evt.preventDefault();

  const self = $(this);
  disableForm(self);

  errorAlert.hide();
  decryptResultDiv.hide();

  const secretKey = $("#decrypt-key").val();
  const pwd = $("#decrypt-pwd").val();

  getSecret(secretKey)
    .then((cipherText) => {
      return decryptSecret(pwd, cipherText);
    })
    .then((secret) => {
      updateDecryptResults(secret);
      enableForm(self);
    })
    .catch((ex) => {
      console.error(ex);
      errorAlert.text(ex.toString()).show();
      enableForm(self);
    });
});
