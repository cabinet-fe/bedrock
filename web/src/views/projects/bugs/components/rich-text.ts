const RICH_TAG = /<\/?(p|br|div|h[1-6]|ul|ol|li|blockquote|pre|strong|b|em|i|u|s|a|img)\b/i;

// Legacy plain-text descriptions and extension submissions have no HTML tags.
export function isRichText(content: string | null | undefined): boolean {
  return !!content && RICH_TAG.test(content);
}

// Strips tags to check whether rich content carries any visible text.
export function richTextToPlain(html: string): string {
  return html.replace(/<[^>]*>/g, "").trim();
}

// Whether rich content carries anything worth saving: visible text or an
// embedded image (image-only descriptions are valid).
export function hasRichContent(html: string): boolean {
  return richTextToPlain(html) !== "" || /<img\b/i.test(html);
}

function escapeHtml(text: string): string {
  return text.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

// Wraps legacy plain-text content into paragraphs so the editor keeps line
// breaks when editing old bugs.
export function toEditorHtml(content: string): string {
  if (!content) return "";
  if (isRichText(content)) return content;
  return content
    .split(/\n/)
    .map((line) => `<p>${line ? escapeHtml(line) : "<br>"}</p>`)
    .join("");
}
