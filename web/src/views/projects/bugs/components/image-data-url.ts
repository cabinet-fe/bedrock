// Compresses a pasted/dropped image into a self-contained data URL so rich
// text descriptions render without extra fetches or auth (attachment download
// endpoints require an Authorization header, which <img> cannot send).
export async function compressImageToDataUrl(
  file: File,
  maxEdge = 1080,
  quality = 0.8,
): Promise<string> {
  const bitmap = await createImageBitmap(file).catch(() => null);
  if (!bitmap) return fileToDataUrl(file);

  const scale = Math.min(1, maxEdge / Math.max(bitmap.width, bitmap.height));
  const width = Math.max(1, Math.round(bitmap.width * scale));
  const height = Math.max(1, Math.round(bitmap.height * scale));
  const canvas = document.createElement("canvas");
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext("2d");
  if (!ctx) {
    bitmap.close();
    return fileToDataUrl(file);
  }

  // JPEG has no alpha; fill white first so transparent screenshots don't go black.
  ctx.fillStyle = "#ffffff";
  ctx.fillRect(0, 0, width, height);
  ctx.drawImage(bitmap, 0, 0, width, height);
  bitmap.close();
  return canvas.toDataURL("image/jpeg", quality);
}

function fileToDataUrl(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      // readAsDataURL always yields a string; the union is FileReader's
      // shared result type.
      if (typeof reader.result === "string") resolve(reader.result);
      else reject(new Error("unsupported file"));
    };
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(file);
  });
}
