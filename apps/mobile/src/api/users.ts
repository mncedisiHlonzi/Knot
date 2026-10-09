/**
 * The user endpoints of the Knot HTTP API.
 *
 * Today this is just the avatar upload, which is a multipart POST rather than a
 * JSON one, so it goes through the client's `uploadFile` helper instead of
 * `request`.
 */
import { User, uploadFile } from './client';

/** The body returned by POST /users/me/avatar: the whole updated user. */
type AvatarResponse = {
  readonly user: User;
};

/** The file extensions the backend accepts for an avatar, keyed by MIME type. */
const AVATAR_EXTENSIONS: Readonly<Record<string, string>> = {
  'image/jpeg': 'jpg',
  'image/png': 'png',
  'image/webp': 'webp',
};

/** Picks a filename for the multipart upload; the server re-sniffs the bytes. */
function avatarFileName(mimeType: string): string {
  return `avatar.${AVATAR_EXTENSIONS[mimeType] ?? 'jpg'}`;
}

/**
 * POST /users/me/avatar — uploads or replaces the authenticated user's avatar.
 *
 * The owner is taken from `token` by the server; it is never sent in the body.
 * The response carries the updated user, so the caller can re-render with the new
 * avatar URL without a second request.
 */
export function uploadAvatar(uri: string, mimeType: string, token: string): Promise<User> {
  return uploadFile<AvatarResponse>('/users/me/avatar', {
    file: { uri, name: avatarFileName(mimeType), type: mimeType },
    token,
  }).then((response) => response.user);
}
