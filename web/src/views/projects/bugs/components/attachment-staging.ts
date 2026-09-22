const GENERIC_CLIPBOARD_IMAGE = /^image\.(png|jpe?g|gif|webp)$/i;

function screenshotName(ext: string): string {
  const now = new Date();
  const pad = (n: number) => String(n).padStart(2, "0");
  const stamp = `${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}-${pad(now.getHours())}${pad(now.getMinutes())}${pad(now.getSeconds())}`;
  return `screenshot-${stamp}${ext}`;
}

// Extracts image files from a paste event; returns null for plain-text pastes
// so the browser keeps its default behavior.
export function clipboardImages(event: ClipboardEvent): File[] | null {
  const files = Array.from(event.clipboardData?.files ?? []).filter((f) =>
    f.type.startsWith("image/"),
  );
  if (!files.length) return null;
  event.preventDefault();
  return files.map((f) => {
    if (!GENERIC_CLIPBOARD_IMAGE.test(f.name)) return f;
    return new File([f], screenshotName(f.name.slice(f.name.lastIndexOf("."))), {
      type: f.type,
    });
  });
}
