import { useState } from 'react';
import { uploadUrl } from '../api';

/**
 * Small round photo of an employee, for lists and cards: a quick look at who
 * someone is. Prefers the latest face photo from the attendance devices
 * (face_photo_path), then the photo HR uploaded (photo_path), then the first
 * letter of the name. A photo that fails to load falls back to the letter
 * rather than a broken-image icon.
 */
export default function PersonAvatar({ name, facePhotoPath, photoPath, size = 32 }) {
  const path = facePhotoPath || photoPath;
  const [failedPath, setFailedPath] = useState(null);
  const showImage = path && failedPath !== path;

  const box = {
    width: size,
    height: size,
    minWidth: size,
    borderRadius: '50%',
    border: '1px solid #e8e8e8',
  };

  if (showImage) {
    return (
      <img
        src={uploadUrl(path)}
        alt={name || ''}
        title={name || ''}
        loading="lazy"
        onError={() => setFailedPath(path)}
        style={{ ...box, objectFit: 'cover', display: 'block' }}
      />
    );
  }
  return (
    <div
      title={name || ''}
      style={{
        ...box,
        background: '#eef1f6',
        color: '#8a93a6',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        fontSize: Math.round(size * 0.4),
        fontWeight: 600,
      }}
    >
      {(name || '?').charAt(0).toUpperCase()}
    </div>
  );
}
