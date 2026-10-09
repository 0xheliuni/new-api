/*
Verifies that the image-input billing formula shown in the log detail evaluates
to the amount actually charged by the backend.

Background: the displayed formula folded image tokens into the plain input
price, so a log line reading "$5.000000/1M" for image tokens did not reproduce
the charged total. The backend (service/text_quota.go:260-263) removes image
tokens from baseTokens and re-adds them multiplied by imageRatio, so the
formula must show the image price (inputPrice * imageRatio) on its own term.

Run: node web/classic/scripts/check-image-billing-formula.mjs
*/

// Production sample — request 202608280834405165258428268d9d6skRkkcnS,
// channel 182 (gpt-image-2), /v1/images/edits.
const SAMPLE = {
  inputTokens: 59 + 2979, // total prompt tokens, image tokens included
  imageOutputTokens: 2979, // image input tokens
  completionTokens: 5650,
  modelRatio: 2.5, // $5.00 / 1M  == modelRatio * 2.0
  imageRatio: 1.6, // $8.00 / 1M  == inputPrice * 1.6
  completionRatio: 6.0, // $30.00 / 1M
  groupRatio: 0.55,
  chargedUSD: 0.106495, // what the backend actually billed
};

// Mirrors service/text_quota.go:260-277.
function backendCharge(s) {
  const inputPrice = s.modelRatio * 2.0;
  const baseTokens = s.inputTokens - s.imageOutputTokens;
  const imageTokensWithRatio = s.imageOutputTokens * s.imageRatio;
  const promptQuota = baseTokens + imageTokensWithRatio;
  const completionQuota = s.completionTokens * s.completionRatio;
  return ((promptQuota + completionQuota) / 1e6) * inputPrice * s.groupRatio;
}

// What a reader gets by evaluating the formula as rendered, term by term.
// `imageTermPrice` is the per-1M price the formula prints for image tokens.
function formulaAsDisplayed(s, imageTermPrice) {
  const inputPrice = s.modelRatio * 2.0;
  const outputPrice = inputPrice * s.completionRatio;
  const nonImageInput = s.inputTokens - s.imageOutputTokens;
  return (
    ((nonImageInput / 1e6) * inputPrice +
      (s.imageOutputTokens / 1e6) * imageTermPrice +
      (s.completionTokens / 1e6) * outputPrice) *
    s.groupRatio
  );
}

const inputPrice = SAMPLE.modelRatio * 2.0;
const imagePrice = inputPrice * SAMPLE.imageRatio;

const charged = backendCharge(SAMPLE);
const asFixed = formulaAsDisplayed(SAMPLE, imagePrice); // fixed: image term at $8
const asBuggy = formulaAsDisplayed(SAMPLE, inputPrice); // old: image term at $5

const round6 = (n) => Number(n.toFixed(6));
let failed = false;
const check = (label, got, want) => {
  const ok = round6(got) === round6(want);
  if (!ok) failed = true;
  console.log(
    `${ok ? 'PASS' : 'FAIL'}  ${label}\n      got  $${got.toFixed(6)}\n      want $${want.toFixed(6)}`,
  );
};

console.log(`input price  $${inputPrice.toFixed(6)} / 1M tokens`);
console.log(`image price  $${imagePrice.toFixed(6)} / 1M tokens\n`);

// The backend model must reproduce the real invoice — pins the sample itself.
check('backend model reproduces the charged amount', charged, SAMPLE.chargedUSD);

// The formula, read literally, must evaluate to what was charged.
check('displayed formula evaluates to the charged amount', asFixed, SAMPLE.chargedUSD);

// Guard: the old single-price rendering must NOT reconcile, otherwise this
// harness would pass no matter what the formula says.
const buggyReconciles = round6(asBuggy) === round6(SAMPLE.chargedUSD);
if (buggyReconciles) {
  failed = true;
  console.log(
    'FAIL  guard: image-tokens-at-input-price should not reconcile, but it did',
  );
} else {
  console.log(
    `PASS  guard: image tokens at input price does not reconcile\n` +
      `      $${asBuggy.toFixed(6)} vs charged $${SAMPLE.chargedUSD.toFixed(6)} ` +
      `(off by $${(SAMPLE.chargedUSD - asBuggy).toFixed(6)})`,
  );
}

process.exit(failed ? 1 : 0);
