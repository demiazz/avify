# avify

The pretty simple batch converter from GIF, JPEG, PNG and WEBP to AVIF.

## Motivation

I've many reference libraries for drawing, and don't require high quality of images, and want to keep space on my local
and remote storages in the same time.

The AVIF format allows to archive that.

## Details

The utility pretty simple and stupid. It's used [libvips](https://github.com/libvips/libvips) under the hood.

It's convert to AVIF with quality 80. It allows to keep size small, and don't lose too many details.
