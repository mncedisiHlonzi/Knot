import React, { useCallback, useEffect, useMemo, useRef } from 'react';
import {
  Animated,
  Alert,
  Modal,
  PanResponder,
  Platform,
  Pressable,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import { ImagePickerResponse, launchCamera, launchImageLibrary } from 'react-native-image-picker';
import { Camera, Images, type Icon } from 'phosphor-react-native';

import type { MediaSource } from '../api/stories';
import { useFade } from '../motion/useFade';
import { MEDIA_LIMIT_MESSAGE } from '../utils/mediaLimit';
import {
  elevation,
  fontWeights,
  radius,
  spacing,
  typeScale,
  useTheme,
  useThemedStyles,
} from '../theme';
import type { ThemeColors } from '../theme';

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
 * What a camera request may ask for.
 *
 * `mixed` is only meaningful where the native camera offers both modes; Android
 * needs the kind chosen before the intent is built (KNOT-053).
 */
type CameraKind = 'photo' | 'video' | 'mixed';

/** The option tile's glyph size. */
const OPTION_ICON_SIZE = 28;

/**
 * Whether an asset the picker returned is a video.
 *
 * A mixed pick no longer tells the sheet what it asked for — one library pass can
 * return photographs and clips together — so each asset is classified from what
 * it reports: its MIME type first, then the duration only a video carries
 * (KNOT-052).
 */
function assetKind(asset: { readonly type?: string; readonly duration?: number }): PickedMediaType {
  const mime = typeof asset.type === 'string' ? asset.type.toLowerCase() : '';
  if (mime.startsWith('video/')) {
    return 'video';
  }
  if (typeof asset.duration === 'number' && asset.duration > 0) {
    return 'video';
  }
  return 'image';
}

/**
 * A bottom sheet offering the two ways to add media: the camera, or the library.
 *
 * It used to draw four rows — take a photo, record a video, choose a photo,
 * choose a video — which asked the reader to make the same decision twice: where
 * the file comes from, and what kind of file it is. Both native pickers can
 * answer the second question themselves, so one pass now returns photographs and
 * clips together and each asset is classified as it arrives (KNOT-052).
 *
 * Every path goes through `react-native-image-picker`; the resulting `source` is
 * what drives the capture badge later. The sheet uses theme tokens throughout —
 * no hardcoded colours.
 */
export default function MediaPickerSheet({
  visible,
  onClose,
  onPicked,
  onError,
  allowVideo = true,
  maxSelection = 1,
}: MediaPickerSheetProps): React.ReactElement {
  const styles = useThemedStyles(createStyles);
  /**
   * Turns a picker response into the picked media, or reports why it could not.
   *
   * A gallery pick can return several assets of mixed kinds; every one that has a
   * uri is reported in a single call, so the caller applies its caps to the whole
   * selection at once rather than item by item.
   */
  const handleResponse = useCallback(
    (response: ImagePickerResponse, source: MediaSource): void => {
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
        const kind = assetKind(asset);
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

  /**
   * Launches the device camera for one kind of media.
   *
   * `mixed` asks the native camera for both photographs and clips, so the camera's
   * own mode control decides the kind. An avatar is an image only, so that flow
   * asks for `photo`.
   */
  const runCamera = useCallback(
    (kind: CameraKind): void => {
      void launchCamera({
        mediaType: allowVideo ? kind : 'photo',
        quality: 0.8,
        videoQuality: 'high',
        saveToPhotos: false,
      })
        .then((response) => handleResponse(response, 'camera'))
        .catch(reportFailure)
        .finally(onClose);
    },
    [allowVideo, handleResponse, onClose, reportFailure],
  );

  /**
   * Opens the camera, choosing a still or a clip first where the platform needs it.
   *
   * On iOS the camera itself offers both modes, so one `mixed` request is enough.
   * Android's picker cannot: `launchCamera` routes every request that is not
   * `video` to the still-camera intent, so a `mixed` capture silently means a
   * photograph (KNOT-052's known issue). Rather than pretend otherwise, the choice
   * is asked here — one tap later than the sheet, and never in place of it
   * (KNOT-053).
   */
  const openCamera = useCallback((): void => {
    if (!allowVideo) {
      runCamera('photo');
      return;
    }

    if (Platform.OS !== 'android') {
      runCamera('mixed');
      return;
    }

    Alert.alert('Camera', 'What would you like to capture?', [
      { text: 'Take Photo', onPress: () => runCamera('photo') },
      { text: 'Record Video', onPress: () => runCamera('video') },
      { text: 'Cancel', style: 'cancel' },
    ]);
  }, [allowVideo, runCamera]);

  /**
   * Launches the photo library for photographs, clips, or both.
   *
   * `selectionLimit` caps how many the library may return. The library treats 0
   * as "unlimited", so a caller with no free slots is refused rather than allowed
   * to pick without bound (KNOT-ADR-054).
   */
  const runLibrary = useCallback((): void => {
    if (maxSelection < 1) {
      onError?.(MEDIA_LIMIT_MESSAGE);
      onClose();
      return;
    }

    void launchImageLibrary({
      mediaType: allowVideo ? 'mixed' : 'photo',
      quality: 0.8,
      videoQuality: 'high',
      selectionLimit: maxSelection,
    })
      .then((response) => handleResponse(response, 'gallery'))
      .catch(reportFailure)
      .finally(onClose);
  }, [allowVideo, handleResponse, maxSelection, onClose, onError, reportFailure]);

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

  // The sheet fades in on open with the shared primitive (KNOT-020k). The fade
  // OUT is left to the `Modal`: `visible={false}` removes the native window in the
  // same frame, so a JS exit animation would be cut off — disclosed.
  const fade = useFade(false);
  useEffect(() => {
    if (visible) {
      fade.fadeIn();
    }
  }, [fade, visible]);

  return (
    <Modal visible={visible} transparent animationType="slide" onRequestClose={close}>
      <Pressable
        style={styles.backdrop}
        onPress={close}
        accessibilityRole="button"
        accessibilityLabel="Close the media picker"
      >
        <Animated.View
          style={[styles.sheet, { opacity: fade.opacity, transform: [{ translateY: dragY }] }]}
        >
          {/* The handle is the drag target: a downward fling dismisses the
              sheet, anything else springs it back. */}
          <View style={styles.handleArea} {...sheetPan.panHandlers}>
            <View style={styles.handle} />
          </View>

          <Pressable style={styles.body} onPress={() => undefined}>
            {/* Two answers, side by side: where the media comes from. What KIND it
                is is the native picker's decision, not a second row's (KNOT-052). */}
            <View style={styles.options}>
              <OptionTile
                icon={Camera}
                label="Camera"
                hint={allowVideo ? 'Photo or video' : 'A photo'}
                onPress={openCamera}
                testID="media-picker-camera"
              />
              <OptionTile
                icon={Images}
                label="Gallery"
                hint={allowVideo ? 'Photos and videos' : 'A photo'}
                onPress={runLibrary}
                testID="media-picker-gallery"
              />
            </View>

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

/**
 * One of the sheet's two choices: a large tile with a clean glyph and its name.
 *
 * The tile is a single touch target (the whole box, not just the glyph) so the
 * sheet reads as two doors rather than a menu.
 */
function OptionTile({
  icon,
  label,
  hint,
  onPress,
  testID,
}: {
  readonly icon: Icon;
  readonly label: string;
  readonly hint: string;
  readonly onPress: () => void;
  readonly testID: string;
}): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  const IconComponent = icon;
  return (
    <Pressable
      style={styles.option}
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={`${label}, ${hint.toLowerCase()}`}
      testID={testID}
    >
      <IconComponent color={colors.text.primary} size={OPTION_ICON_SIZE} weight="bold" />
      <Text style={[typeScale.button, styles.optionLabel]}>{label}</Text>
      <Text style={[typeScale.caption, styles.optionHint]}>{hint}</Text>
    </Pressable>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
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
      color: colors.state.danger,
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
    // One of the two choices: a tall tile that is entirely a touch target, so the
    // sheet reads as two doors rather than a list of rows (KNOT-052).
    option: {
      alignItems: 'center',
      backgroundColor: colors.bg.sunken,
      borderRadius: radius.lg,
      flex: 1,
      gap: spacing.xs,
      justifyContent: 'center',
      minHeight: 116,
      paddingHorizontal: spacing.md,
      paddingVertical: spacing.lg,
    },
    optionHint: {
      color: colors.text.tertiary,
      textAlign: 'center',
    },
    optionLabel: {
      color: colors.text.primary,
      marginTop: spacing.xs,
    },
    // The pair sits side by side, sharing the row evenly.
    options: {
      flexDirection: 'row',
      gap: spacing.md,
    },
    // The sheet: rounded top corners, the canvas fill, and the floating-bar shadow
    // (elevation 3, whose offset points up).
    sheet: {
      ...elevation[3],
      backgroundColor: colors.bg.sheet,
      borderTopLeftRadius: radius.xxl,
      borderTopRightRadius: radius.xxl,
      paddingBottom: spacing.xl,
    },
  });
