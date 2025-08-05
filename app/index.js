// allow jquery to take over elements hidden by css during page load
$(".initially-hidden").hide().removeClass("initially-hidden");

const errorAlert = $("#error-alert");
const encryptResultDiv = $("#encrypt-result");
const decryptResultDiv = $("#decrypt-result");
const decryptKey = $("#decrypt-key");

decryptKey.on("input", function () {
  if (this.validity.patternMismatch) {
    this.setCustomValidity("Invalid shared key.");
  } else {
    this.setCustomValidity("");
  }
});

function disableForm(form) {
  form.find("fieldset").prop("disabled", true);
}

function enableForm(form) {
  form.find("fieldset").prop("disabled", false);
}

function createPassword() {
  return window.crypto.randomUUID().replaceAll("-", "");
}

function createDecryptLink(pwd, key) {
  return `${window.location.origin}${window.location.pathname}#${pwd}-${key}`;
}

function parseDecryptKey() {
  const [pwd, key] = decryptKey.val().split("-");
  return { pwd, key };
}

function setDecryptKeyFromLocation() {
  const hash = window.location.hash;
  if (!!hash) {
    decryptKey.val(hash.substring(1));
    return true;
  }
  return false;
}

function updateEncryptResults(pwd, key, ttl) {
  const link = createDecryptLink(pwd, key);
  const ttlTxt = ttl === "1" ? "1 hour" : `${ttl} hours`;
  const expiry = Date.now() + parseInt(ttl) * 60 * 60 * 1000;
  const expiryTxt = new Date(expiry).toLocaleString();

  encryptResultDiv.find(".copy-me").text(link);
  encryptResultDiv.find(".expire-in").text(ttlTxt);
  encryptResultDiv.find(".expire-at").text(expiryTxt);
  encryptResultDiv.show();
}

function updateDecryptResults(secret) {
  decryptResultDiv.find(".copy-me").text(secret);
  decryptResultDiv.show();
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

$("#tab-row button").on("shown.bs.tab", function (evt) {
  const tabTarget = $(evt.target).data("bs-target");
  $(tabTarget).find(".focus-target").trigger("focus");
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

$("#encrypt-tab form").on("submit", function (evt) {
  evt.preventDefault();

  const secret = $("#encrypt-value").val();
  const ttl = $("#encrypt-ttl").val();
  const pwd = createPassword();
  const self = $(this);

  disableForm(self);
  errorAlert.hide();
  encryptResultDiv.hide();

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

  const shared = parseDecryptKey();
  const self = $(this);

  disableForm(self);
  errorAlert.hide();
  decryptResultDiv.hide();

  getSecret(shared.key)
    .then((cipherText) => {
      return decryptSecret(shared.pwd, cipherText);
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

if (setDecryptKeyFromLocation()) {
  $("#encrypt-tab-btn, #encrypt-tab").removeClass("active");
  $("#decrypt-tab-btn, #decrypt-tab").addClass("active");
  $("#decrypt-tab .focus-target").trigger("focus");
} else {
  $("#encrypt-tab .focus-target").trigger("focus");
}
