/**
 * Returns a safe basename for use in APIs and storage.
 * On Windows, File.name can be a full path (e.g. "C:\Users\...\file.mp3");
 * this strips any path and returns only the filename for cross-platform reliability.
 */
export function getSafeBasename(name: string | undefined | null): string {
  if (name == null || typeof name !== 'string') return 'audio';
  const normalized = name.replace(/\\/g, '/');
  const last = normalized.split('/').pop();
  return last && last.length > 0 ? last : 'audio';
}
