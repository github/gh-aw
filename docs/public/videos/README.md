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

Poster images (video thumbnails) provide a better user experience by showing a preview frame before the video loads. Poster images are automatically detected by the Video component - they should be placed alongside video files with the same name but a `.png` extension.

For example:
- `create-workflow.mp4` → `create-workflow.png` (poster)
- `demo.mp4` → `demo.png` (poster)

To generate poster images for all videos in this directory:

```bash
# From the repository root
./scripts/generate-video-posters.sh
```

This script will:
- Extract a frame at 1 second from each MP4 video
- Generate high-quality PNG poster images (1920x1080)
- Save them alongside the video files with `.png` extension

The Video component automatically detects and uses these poster images:

```mdx
<Video 
  src="/gh-aw/videos/demo.mp4"
  caption="Demo video"
/>
```

No need to specify the `thumbnail` prop - it's automatically derived from the video path!

## Example

To add a new video to the documentation:

1. Place the video file in this directory: `docs/public/videos/demo.mp4`
2. Generate the poster image: `./scripts/generate-video-posters.sh`
3. Reference it in your MDX file:

```mdx
import Video from '@components/Video.astro';

<Video 
  src="/gh-aw/videos/demo.mp4" 
  caption="Workflow Demo"
  aspectRatio="16:9"
/>
```

The poster image (`demo.png`) will be automatically detected and used.
