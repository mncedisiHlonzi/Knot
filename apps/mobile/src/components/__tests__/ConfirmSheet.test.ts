import Button from '../Button';
import ConfirmSheet from '../ConfirmSheet';

/**
 * `ConfirmSheet` renders a `Modal`, two `Button`s and plain views. This project
 * has no renderer and must not add one, so React's hook primitives are stubbed and
 * the component is called as a plain function: `useCallback` returns its callback
 * unchanged, `useEffect` does nothing (the commit guard is only reset on a real
 * re-render), and `useRef` hands back a fresh mutable box (KNOT-032-fix1).
 */
jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useCallback: (callback: unknown) => callback,
  useEffect: () => undefined,
  useRef: (value: unknown) => ({ current: value }),
}));

jest.mock('react-native', () => ({
  Modal: 'Modal',
  Pressable: 'Pressable',
  StyleSheet: { create: (styles: unknown) => styles },
  Text: 'Text',
  View: 'View',
}));

type ElementLike = { type?: unknown; props?: Record<string, unknown> };

/** Collects every element of a given type in a directly-called component's tree. */
function elementsByType(node: unknown, type: unknown, found: ElementLike[] = []): ElementLike[] {
  if (Array.isArray(node)) {
    for (const child of node) {
      elementsByType(child, type, found);
    }
    return found;
  }
  if (node === null || node === undefined || typeof node !== 'object') {
    return found;
  }

  const element = node as ElementLike;
  if (element.type === type) {
    found.push(element);
  }
  elementsByType(element.props?.children, type, found);

  return found;
}

/** Collects every string drawn anywhere in the tree. */
function collectText(node: unknown, found: string[] = []): string[] {
  if (typeof node === 'string') {
    found.push(node);
    return found;
  }
  if (Array.isArray(node)) {
    for (const child of node) {
      collectText(child, found);
    }
    return found;
  }
  if (node === null || node === undefined || typeof node !== 'object') {
    return found;
  }

  collectText((node as ElementLike).props?.children, found);
  return found;
}

/** The sheet's button carrying the given label. */
function button(tree: unknown, label: string): ElementLike | undefined {
  return elementsByType(tree, Button).find((candidate) => candidate.props?.label === label);
}

/** Presses an element's handler the way a finger would. */
function press(element: ElementLike | undefined): void {
  (element?.props?.onPress as (() => void) | undefined)?.();
}

const COPY = {
  visible: true,
  title: 'Hide this story?',
  message: 'This story will be removed from your feed for now.',
  confirmLabel: 'Hide story',
  cancelLabel: 'Keep story',
} as const;

describe('ConfirmSheet', () => {
  it('asks the question, explains the effect, and offers both choices', () => {
    const element = ConfirmSheet({
      ...COPY,
      destructive: true,
      onConfirm: jest.fn(),
      onCancel: jest.fn(),
    });

    const text = collectText(element);
    expect(text).toContain(COPY.title);
    expect(text).toContain(COPY.message);

    // Both answers exist, and the safe one is the only one that is not destructive.
    expect(button(element, COPY.cancelLabel)).toBeDefined();
    expect(button(element, COPY.confirmLabel)).toBeDefined();
    expect(button(element, COPY.cancelLabel)?.props?.variant).toBe('secondary');
    expect(button(element, COPY.confirmLabel)?.props?.variant).toBe('danger');
  });

  it('keeps things as they are when the safe choice is taken', () => {
    const onConfirm = jest.fn();
    const onCancel = jest.fn();

    const element = ConfirmSheet({ ...COPY, onConfirm, onCancel });
    press(button(element, COPY.cancelLabel));

    expect(onCancel).toHaveBeenCalledTimes(1);
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it('applies the choice once, even when the confirm action is pressed twice', () => {
    const onConfirm = jest.fn();
    const onCancel = jest.fn();

    const element = ConfirmSheet({ ...COPY, destructive: true, onConfirm, onCancel });
    const confirm = button(element, COPY.confirmLabel);

    press(confirm);
    press(confirm);

    // A double tap cannot apply the caller's effect twice (KNOT-032-fix1).
    expect(onConfirm).toHaveBeenCalledTimes(1);
    expect(onCancel).not.toHaveBeenCalled();
  });

  it('dismisses without confirming when the backdrop is tapped', () => {
    const onConfirm = jest.fn();
    const onCancel = jest.fn();

    const element = ConfirmSheet({ ...COPY, onConfirm, onCancel });
    const [backdrop] = elementsByType(element, 'Pressable');

    expect(backdrop?.props?.accessibilityLabel).toBe('Dismiss the confirmation');
    press(backdrop);

    expect(onCancel).toHaveBeenCalledTimes(1);
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it('closes on the Android back button without confirming', () => {
    const onConfirm = jest.fn();
    const onCancel = jest.fn();

    const element = ConfirmSheet({ ...COPY, onConfirm, onCancel });
    const [modal] = elementsByType(element, 'Modal');
    const requestClose = modal?.props?.onRequestClose as (() => void) | undefined;

    // `onRequestClose` is what the platform calls for the hardware back button.
    requestClose?.();

    expect(onCancel).toHaveBeenCalledTimes(1);
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it('stays hidden, and uncommitted, while it is not visible', () => {
    const onConfirm = jest.fn();
    const onCancel = jest.fn();

    const element = ConfirmSheet({ ...COPY, visible: false, onConfirm, onCancel });
    const [modal] = elementsByType(element, 'Modal');

    expect(modal?.props?.visible).toBe(false);
    expect(onConfirm).not.toHaveBeenCalled();
    expect(onCancel).not.toHaveBeenCalled();
  });

  it('draws a non-destructive confirmation in the primary ink', () => {
    const element = ConfirmSheet({ ...COPY, onConfirm: jest.fn(), onCancel: jest.fn() });

    expect(button(element, COPY.confirmLabel)?.props?.variant).toBe('primary');
  });

  it('names the sheet surface for the screen that opened it', () => {
    const element = ConfirmSheet({
      ...COPY,
      onConfirm: jest.fn(),
      onCancel: jest.fn(),
      testID: 'hide-confirm',
    });

    const named = elementsByType(element, 'Pressable').filter(
      (view) => view.props?.testID === 'hide-confirm',
    );

    expect(named).toHaveLength(1);
  });
});
