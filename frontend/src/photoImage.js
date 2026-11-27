// Client-side photo preparation. The server re-processes every upload (it sniffs
// the bytes, rotates, strips EXIF and GPS, re-encodes to about 400 px) and rejects
// anything over 5 MiB or 25 megapixels, which many phone cameras exceed. So the
// browser shrinks the photo first: camera photos never hit the limit, uploads are
// quick on mobile data, and EXIF never leaves the device. The server stays the
// authority; this is only a courtesy.

export const MAX_SIDE = 1200; // longest side after shrinking, in pixels
export const JPEG_QUALITY = 0.85;
export const MAX_SOURCE_BYTES = 40 * 1024 * 1024; // refuse absurd files before decoding them
export const ACCEPTED_TYPES = ["image/jpeg", "image/png", "image/webp"];

export class PhotoError extends Error {}

/** Dimensions after scaling so the longest side is at most maxSide. Never enlarges. */
export function fitWithin(width, height, maxSide = MAX_SIDE) {
  if (!(width > 0) || !(height > 0)) return { width: 0, height: 0 };
  const scale = Math.min(1, maxSide / Math.max(width, height));
  return { width: Math.max(1, Math.round(width * scale)), height: Math.max(1, Math.round(height * scale)) };
}

/** A friendly message when the chosen file can't be used, or null when it can. */
export function checkPhotoFile(file) {
  if (!file) return "Choose a photo first.";
  // An empty type is allowed: some browsers don't report one, and decoding decides.
  if (file.type && !ACCEPTED_TYPES.includes(file.type)) return "Choose a JPEG, PNG or WebP photo.";
  if (file.size > MAX_SOURCE_BYTES) return "That file is too large. Choose a photo under 40 MB.";
  if (file.size === 0) return "That file is empty. Choose another photo.";
  return null;
}

async function decode(file) {
  if (typeof createImageBitmap === "function") {
    // "from-image" applies the camera's EXIF orientation, so portrait photos stay upright.
    return createImageBitmap(file, { imageOrientation: "from-image" });
  }
  const url = URL.createObjectURL(file);
  try {
    const image = new Image();
    image.src = url;
    await image.decode();
    return image;
  } finally {
    URL.revokeObjectURL(url);
  }
}

/** Scale the photo down and re-encode it as a JPEG Blob ready to upload. */
export async function shrinkPhoto(file, { maxSide = MAX_SIDE, quality = JPEG_QUALITY } = {}) {
  const problem = checkPhotoFile(file);
  if (problem) throw new PhotoError(problem);
  let source;
  try {
    source = await decode(file);
  } catch {
    throw new PhotoError("This photo couldn’t be read. Try a JPEG or PNG.");
  }
  try {
    const width = source.naturalWidth || source.width;
    const height = source.naturalHeight || source.height;
    const size = fitWithin(width, height, maxSide);
    if (!size.width) throw new PhotoError("This photo couldn’t be read. Try a JPEG or PNG.");
    const canvas = document.createElement("canvas");
    canvas.width = size.width;
    canvas.height = size.height;
    const context = canvas.getContext("2d");
    // A transparent PNG would turn black as a JPEG, so paint white underneath.
    context.fillStyle = "#fff";
    context.fillRect(0, 0, size.width, size.height);
    context.drawImage(source, 0, 0, size.width, size.height);
    const blob = await new Promise((resolve) => canvas.toBlob(resolve, "image/jpeg", quality));
    if (!blob) throw new PhotoError("This photo couldn’t be prepared. Try another one.");
    return blob;
  } finally {
    source.close?.();
  }
}
