#!/usr/bin/env bash
set -e

IMAGE_FILTERS_RELEASE=v0.1.2

LINUX_ZIP_FILENAME="imf_${IMAGE_FILTERS_RELEASE}_linux.zip"
WIN_ZIP_FILENAME="imf_${IMAGE_FILTERS_RELEASE}_win.zip"

LINUX_ZIP_URL="https://github.com/golden-vcr/image-filters/releases/download/${IMAGE_FILTERS_RELEASE}/${LINUX_ZIP_FILENAME}"
WIN_ZIP_URL="https://github.com/golden-vcr/image-filters/releases/download/${IMAGE_FILTERS_RELEASE}/${WIN_ZIP_FILENAME}"

rm -rf external/bin
mkdir -p external/bin
curl -L -o "external/bin/$LINUX_ZIP_FILENAME" "$LINUX_ZIP_URL"
curl -L -o "external/bin/$WIN_ZIP_FILENAME" "$WIN_ZIP_URL"
(cd external/bin && unzip "$LINUX_ZIP_FILENAME" && rm "$LINUX_ZIP_FILENAME")
(cd external/bin && unzip "$WIN_ZIP_FILENAME" && rm "$WIN_ZIP_FILENAME")
chmod +x external/bin/imf
