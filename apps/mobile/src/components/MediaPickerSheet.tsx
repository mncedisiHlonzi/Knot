import React, { useCallback } from 'react';
import { Modal, Pressable, StyleSheet, Text } from 'react-native';
import { ImagePickerResponse, launchCamera, launchImageLibrary } from 'react-native-image-picker';

import type { MediaSource } from '../api/stories';
import { colors, fontSizes, fontWeights, radius, spacing } from '../theme';

/** The kind of file a picker produced. */
export type PickedMediaType = 'image' | 'video';

/** What the sheet reports back once a file has been chosen or captured. */
export type PickedMedia = {
  readonly uri: string;
  readonly type: PickedMediaType;
  readonly mimeType: string;
  /** Size in bytes, 0 when the picker did not report one. */
  readonly size: number;
  /** Pixel width/height when known. */
  readonly width?: number;
  readonly height?: number;
  /** Video duration in SECONDS when known (the unit the picker reports). */
  readonly duration?: number;
  /** Whether it came from the camera or the gallery. */
  readonly source: MediaSource;
};

type MediaPickerSheetProps = {
  readonly visible: boolean;
  readonly onClose: () => void;
  readonly onPicked: (media: PickedMedia) => void;
  /** Reports a picker failure (permission denied, capture failed) to the caller. */
  readonly onError?: (message: string) => void;
  /**
   * When false, only the two image options are offered. The avatar flow sets it
   * false because an avatar is an image only.
   */
  readonly allowVideo?: boolean;
};

/**
 * A bottom sheet offering to capture or choose an image or a video.
 *
 * All four paths go through `react-native-image-picker`; the only difference is
 * whether the camera or the library is launched, and whether an image or a video
 * is requested. The resulting `source` is what drives the capture badge later.
 * The sheet uses theme tokens throughout — no hardcoded colours.
 */
export default function MediaPickerSheet({
  visible,
  onClose,
  onPicked,
  onError,
  allowVideo = true,
}: MediaPickerSheetProps): React.ReactElement {
  /** Turns a picker response into a PickedMedia, or reports why it could not. */
  const handleResponse = useCallback(
    (response: ImagePickerResponse, kind: PickedMediaType, source: MediaSource): void => {
      if (response.didCancel) {
        return;
      }
      if (response.errorCode !== undefined) {
        onError?.(response.errorMessage ?? 'the picker could not open');
        return;
      }

      const asset = response.assets?.[0];
      if (asset === undefined || asset.uri === undefined) {
        return;
      }

      onPicked({
        uri: asset.uri,
        type: kind,
        mimeType: asset.type ?? (kind === 'video' ? 'video/mp4' : 'image/jpeg'),
        size: asset.fileSize ?? 0,
        ...(asset.width === undefined ? {} : { width: asset.width }),
        ...(asset.height === undefined ? {} : { height: asset.height }),
        ...(asset.duration === undefined ? {} : { duration: asset.duration }),
        source,
      });
    },
    [onError, onPicked],
  );

  const reportFailure = useCallback(
    (error: unknown): void => {
      onError?.(error instanceof Error ? error.message : 'the picker failed');
    },
    [onError],
  );

  /** Launches the device camera for a photo or a video. */
  const runCamera = useCallback(
    (kind: PickedMediaType, source: MediaSource): void => {
      void launchCamera({
        mediaType: kind === 'video' ? 'video' : 'photo',
        quality: 0.8,
        videoQuality: 'high',
        saveToPhotos: false,
      })
        .then((response) => handleResponse(response, kind, source))
        .catch(reportFailure)
        .finally(onClose);
    },
    [handleResponse, onClose, reportFailure],
  );

  /** Launches the photo library for a photo or a video. */
  const runLibrary = useCallback(
    (kind: PickedMediaType, source: MediaSource): void => {
      void launchImageLibrary({
        mediaType: kind === 'video' ? 'video' : 'photo',
        quality: 0.8,
        videoQuality: 'high',
        selectionLimit: 1,
      })
        .then((response) => handleResponse(response, kind, source))
        .catch(reportFailure)
        .finally(onClose);
    },
    [handleResponse, onClose, reportFailure],
  );

  return (
    <Modal visible={visible} transparent animationType="slide" onRequestClose={onClose}>
      <Pressable style={styles.backdrop} onPress={onClose}>
        <Pressable style={styles.sheet} onPress={() => undefined}>
          <Text style={styles.title}>Add media</Text>

          <Option label="Take Photo" onPress={() => runCamera('image', 'camera')} />
          {allowVideo ? (
            <Option label="Record Video" onPress={() => runCamera('video', 'camera')} />
          ) : null}
          <Option
            label="Choose Photo from Gallery"
            onPress={() => runLibrary('image', 'gallery')}
          />
          {allowVideo ? (
            <Option
              label="Choose Video from Gallery"
              onPress={() => runLibrary('video', 'gallery')}
            />
          ) : null}

          <Pressable style={styles.cancel} onPress={onClose} accessibilityRole="button">
            <Text style={styles.cancelText}>Cancel</Text>
          </Pressable>
        </Pressable>
      </Pressable>
    </Modal>
  );
}

/** One tappable row in the sheet. */
function Option({
  label,
  onPress,
}: {
  readonly label: string;
  readonly onPress: () => void;
}): React.ReactElement {
  return (
    <Pressable style={styles.option} onPress={onPress} accessibilityRole="button">
      <Text style={styles.optionText}>{label}</Text>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  backdrop: {
    backgroundColor: colors.overlay,
    flex: 1,
    justifyContent: 'flex-end',
  },
  cancel: {
    alignItems: 'center',
    marginTop: spacing.md,
    paddingVertical: spacing.md,
  },
  cancelText: {
    color: colors.text.secondary,
    fontSize: fontSizes.base,
    fontWeight: fontWeights.semiBold,
  },
  option: {
    backgroundColor: colors.bg.surface,
    borderColor: colors.border.default,
    borderRadius: radius.md,
    borderWidth: 1,
    marginTop: spacing.sm,
    paddingVertical: 14,
  },
  optionText: {
    color: colors.text.primary,
    fontSize: fontSizes.md,
    fontWeight: fontWeights.medium,
    textAlign: 'center',
  },
  sheet: {
    backgroundColor: colors.bg.primary,
    borderTopLeftRadius: radius['2xl'],
    borderTopRightRadius: radius['2xl'],
    padding: spacing.xl,
    paddingBottom: spacing['2xl'],
  },
  title: {
    color: colors.text.primary,
    fontSize: fontSizes.lg,
    fontWeight: fontWeights.bold,
    marginBottom: spacing.sm,
    textAlign: 'center',
  },
});
