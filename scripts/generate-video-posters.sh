#!/usr/bin/env bash
set -e

# Script to generate web-optimized poster images from videos in docs/public/videos
# Posters are extracted at 1 second into the video and saved as PNG files
# alongside the video files (same directory, with .png extension)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
VIDEOS_DIR="$REPO_ROOT/docs/public/videos"

# Check if ffmpeg is installed
if ! command -v ffmpeg &> /dev/null; then
    echo "Error: ffmpeg is not installed. Please install it first:"
    echo "  Ubuntu/Debian: sudo apt-get install ffmpeg"
    echo "  macOS: brew install ffmpeg"
    exit 1
fi

echo "Generating poster images from videos in $VIDEOS_DIR"
echo "Posters will be saved alongside video files with .png extension"
echo ""

# Process each MP4 video file
for video in "$VIDEOS_DIR"/*.mp4; do
    if [ ! -f "$video" ]; then
        echo "No video files found in $VIDEOS_DIR"
        exit 1
    fi
    
    # Get the base filename without extension
    basename=$(basename "$video" .mp4)
    
    # Generate the output poster filename (PNG in same directory as video)
    poster="$VIDEOS_DIR/${basename}.png"
    
    echo "Processing: $basename.mp4"
    echo "  → Extracting frame at 1 second..."
    
    # Extract frame at 1 second, scale to maintain quality
    # -ss 1: Seek to 1 second
    # -i: Input file
    # -vframes 1: Extract only 1 frame
    # -q:v 2: High quality (1-31 scale, 2 is very high quality)
    # -vf scale: Ensure output is proper size
    ffmpeg -ss 1 -i "$video" -vframes 1 -q:v 2 -vf "scale=1920:1080:force_original_aspect_ratio=decrease" "$poster" -y 2>&1 | grep -v "frame=" || true
    
    if [ -f "$poster" ]; then
        size=$(du -h "$poster" | cut -f1)
        echo "  ✓ Generated: $(basename "$poster") ($size)"
    else
        echo "  ✗ Failed to generate poster"
        exit 1
    fi
    echo ""
done

echo "✓ All poster images generated successfully!"
echo ""
echo "Generated files:"
ls -lh "$VIDEOS_DIR"/*.png 2>/dev/null || true
