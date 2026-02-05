# Videos Directory

This directory contains video files used in the documentation.

## Usage

Place video files here and reference them in documentation using the Video component:

```mdx
import Video from '@components/Video.astro';

<Video src="/gh-aw/videos/your-video.mp4" caption="Video Title" />
```

## Supported Formats

- MP4 (`.mp4`) - **Recommended** for best browser compatibility
- WebM (`.webm`) - Modern, open format
- OGG (`.ogg`) - Open format, older browsers
- MOV (`.mov`) - QuickTime format
- AVI (`.avi`) - Legacy format
- MKV (`.mkv`) - Matroska format

## Best Practices

- Keep file sizes reasonable for web delivery (< 50MB recommended)
- Use MP4 with H.264 codec for widest browser support
- Provide meaningful filenames (e.g., `workflow-demo.mp4`)
- Consider adding poster images (thumbnails) for better UX
- Compress videos appropriately for web use

## Generating Poster Images

Poster images (video thumbnails) provide a better user experience by showing a preview frame before the video loads. To generate poster images for all videos in this directory:

```bash
# From the repository root
./scripts/generate-video-posters.sh
```

This script will:
- Extract a frame at 1 second from each MP4 video
- Generate high-quality PNG poster images (1920x1080)
- Save them to `docs/public/images/` with the naming pattern `{video-name}-thumbnail.png`

The generated poster images can then be referenced in the Video component:

```mdx
<Video 
  src="/gh-aw/videos/demo.mp4"
  thumbnail="/gh-aw/images/demo-thumbnail.png"
/>
```

## Example

To add a new video to the documentation:

1. Place the video file in this directory: `docs/public/videos/demo.mp4`
2. Reference it in your MDX file:

```mdx
import Video from '@components/Video.astro';

<Video 
  src="/gh-aw/videos/demo.mp4" 
  caption="Workflow Demo"
  thumbnail="/gh-aw/images/demo-thumbnail.png"
/>
```
