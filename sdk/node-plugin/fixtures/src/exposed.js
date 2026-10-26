export const answer = 42;

export async function loadLazyAnswer() {
  const lazy = await import(/* webpackChunkName: "fixture-lazy" */ "./lazy.js");
  return lazy.lazyAnswer;
}

export default function getAnswerLabel() {
  return "answer";
}
