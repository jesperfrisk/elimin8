`photo-corners.webp` is a generated, lossless 800x480 test image with four solid-color quadrants, matching `quadrantPhoto` in `photos_orientation_test.go`:

- top left: RGB (220, 30, 30)
- top right: RGB (30, 200, 30)
- bottom left: RGB (30, 30, 220)
- bottom right: RGB (220, 200, 30)

It was encoded from a binary RGB PPM with `cwebp -lossless corners.ppm -o photo-corners.webp`. The decoder is already provided by `golang.org/x/image`; no encoder dependency or external tool is needed to run the tests. EXIF metadata is constructed by the tests, which check all eight orientation transforms and stripping of GPS metadata.