import React, { useCallback, useMemo, useRef } from 'react';
import { Animated, Modal, PanResponder, Pressable, StyleSheet, Text, View } from 'react-native';
import { ImagePickerResponse, launchCamera, launchImageLibrary } from 'react-native-image-picker';
import { Camera, FilmStrip, Images, VideoCamera, type Icon } from 'phosphor-react-native';

import type { MediaSource } from '../api/stories';
import { MEDIA_LIMIT_MESSAGE } from '../utils/mediaLimit';
import { colors, elevation, fontWeights, radius, spacing, typeScale } from '../theme';

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
  /**
   * Reports every file that was chosen or captured. The camera yields one; a
   * gallery pick can yield several.
   */
  readonly onPicked: (media: readonly PickedMedia[]) => void;
  /** Reports a picker failure (permission denied, capture failed) to the caller. */
  readonly onError?: (message: string) => void;
  /**
   * When false, only the two image options are offered. The avatar flow sets it
   * false because an avatar is an image only.
   */
  readonly allowVideo?: boolean;
  /**
   * The most files the gallery may return in one pick, so a caller fills only its
   * free slots. The camera always yields one. Defaults to 1, the single-file
   * flows such as an avatar (KNOT-ADR-054).
   */
  readonly maxSelection?: number;
};

/** How far the sheet must be dragged down before it dismisses. */
const SHEET_CLOSE_DISTANCE = 96;

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
  maxSelection = 1,
}: MediaPickerSheetProps): React.ReactElement {
  /**
   * Turns a picker response into the picked media, or reports why it could not.
   *
   * A gallery pick can return several assets; every one that has a uri is
   * reported in a single call, so the caller applies its cap to the whole
   * selection at once rather than item by item.
   */
  const handleResponse = useCallback(
    (response: ImagePickerResponse, kind: PickedMediaType, source: MediaSource): void => {
      if (response.didCancel) {
        return;
      }
      if (response.errorCode !== undefined) {
        onError?.(response.errorMessage ?? 'the picker could not open');
        return;
      }

      const picked: PickedMedia[] = [];
      for (const asset of response.assets ?? []) {
        if (asset.uri === undefined) {
          continue;
        }
        picked.push({
          uri: asset.uri,
          type: kind,
          mimeType: asset.type ?? (kind === 'video' ? 'video/mp4' : 'image/jpeg'),
          size: asset.fileSize ?? 0,
          ...(asset.width === undefined ? {} : { width: asset.width }),
          ...(asset.height === undefined ? {} : { height: asset.height }),
          ...(asset.duration === undefined ? {} : { duration: asset.duration }),
          source,
        });
      }

      if (picked.length > 0) {
        onPicked(picked);
      }
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

  /**
   * Launches the photo library for one or more photos or videos.
   *
   * `selectionLimit` caps how many the library may return. The library treats 0
   * as "unlimited", so a caller with no free slots is refused rather than allowed
   * to pick without bound (KNOT-ADR-054).
   */
  const runLibrary = useCallback(
    (kind: PickedMediaType, source: MediaSource): void => {
      if (maxSelection < 1) {
        onError?.(MEDIA_LIMIT_MESSAGE);
        onClose();
        return;
      }

      void launchImageLibrary({
        mediaType: kind === 'video' ? 'video' : 'photo',
        quality: 0.8,
        videoQuality: 'high',
        selectionLimit: maxSelection,
      })
        .then((response) => handleResponse(response, kind, source))
        .catch(reportFailure)
        .finally(onClose);
    },
    [handleResponse, maxSelection, onClose, onError, reportFailure],
  );

  // The sheet drags down to dismiss from its handle, matching the profile's
  // options sheet (KNOT-020g-fix2).
  const dragY = useRef(new Animated.Value(0)).current;
  const close = useCallback((): void => {
    dragY.setValue(0);
    onClose();
  }, [dragY, onClose]);
  const sheetPan = useMemo(
    () =>
      PanResponder.create({
        onStartShouldSetPanResponder: () => true,
        onMoveShouldSetPanResponder: (_event, gesture) => gesture.dy > 2,
        onPanResponderMove: (_event, gesture) => {
          if (gesture.dy > 0) {
            dragY.setValue(gesture.dy);
          }
        },
        onPanResponderRelease: (_event, gesture) => {
          if (gesture.dy > SHEET_CLOSE_DISTANCE || gesture.vy > 0.5) {
            close();
          } else {
            Animated.spring(dragY, { toValue: 0, useNativeDriver: true }).start();
          }
        },
      }),
    [close, dragY],
  );

  return (
    <Modal visible={visible} transparent animationType="slide" onRequestClose={close}>
      <Pressable
        style={styles.backdrop}
        onPress={close}
        accessibilityRole="button"
        accessibilityLabel="Close the media picker"
      >
        <Animated.View style={[styles.sheet, { transform: [{ translateY: dragY }] }]}>
          {/* The handle is the drag target: a downward fling dismisses the
              sheet, anything else springs it back. */}
          <View style={styles.handleArea} {...sheetPan.panHandlers}>
            <View style={styles.handle} />
          </View>

          <Pressable style={styles.body} onPress={() => undefined}>
            <SheetRow
              icon={Camera}
              label="Take Photo"
              onPress={() => runCamera('image', 'camera')}
            />
            {allowVideo ? (
              <SheetRow
                icon={VideoCamera}
                label="Record Video"
                onPress={() => runCamera('video', 'camera')}
              />
            ) : null}
            <SheetRow
              icon={Images}
              label="Choose Photo from Gallery"
              onPress={() => runLibrary('image', 'gallery')}
            />
            {allowVideo ? (
              <SheetRow
                icon={FilmStrip}
                label="Choose Video from Gallery"
                onPress={() => runLibrary('video', 'gallery')}
              />
            ) : null}

            <Pressable
              style={styles.cancel}
              onPress={close}
              accessibilityRole="button"
              accessibilityLabel="Cancel"
            >
              <Text style={[typeScale.body, styles.cancelText]}>Cancel</Text>
            </Pressable>
          </Pressable>
        </Animated.View>
      </Pressable>
    </Modal>
  );
}

/** One row in the sheet: a leading icon and a bold label. */
function SheetRow({
  icon,
  label,
  onPress,
}: {
  readonly icon: Icon;
  readonly label: string;
  readonly onPress: () => void;
}): React.ReactElement {
  const IconComponent = icon;
  return (
    <Pressable
      style={styles.row}
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={label}
    >
      <IconComponent color={colors.text.tertiary} size={20} weight="bold" />
      <Text style={[typeScale.body, styles.rowLabel]} numberOfLines={1}>
        {label}
      </Text>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  backdrop: {
    backgroundColor: colors.scrim.overlay,
    flex: 1,
    justifyContent: 'flex-end',
  },
  body: {
    paddingHorizontal: spacing.lg,
  },
  // A text-only Cancel below the choices, in the same 44-point rows. Rows are
  // separated by spacing alone — no drawn dividers (KNOT-020g-fix3).
  cancel: {
    alignItems: 'center',
    justifyContent: 'center',
    minHeight: 44,
    paddingVertical: spacing.md,
  },
  cancelText: {
    color: colors.text.secondary,
    fontWeight: fontWeights.bold,
  },
  handle: {
    backgroundColor: colors.border.hairline,
    borderRadius: radius.pill,
    height: 4,
    width: 32,
  },
  handleArea: {
    alignItems: 'center',
    paddingBottom: spacing.sm,
    paddingTop: spacing.sm,
  },
  // One choice: a leading icon and its label, separated from its neighbour by
  // spacing alone (the same row the profile's options sheet draws).
  row: {
    alignItems: 'center',
    flexDirection: 'row',
    gap: spacing.md,
    minHeight: 44,
    paddingVertical: spacing.md,
  },
  rowLabel: {
    color: colors.text.primary,
    flex: 1,
    fontWeight: fontWeights.bold,
  },
  // The sheet: rounded top corners, the canvas fill, and the floating-bar shadow
  // (elevation 3, whose offset points up).
  sheet: {
    ...elevation[3],
    backgroundColor: colors.bg.canvas,
    borderTopLeftRadius: radius.xxl,
    borderTopRightRadius: radius.xxl,
    paddingBottom: spacing.xl,
  },
});
