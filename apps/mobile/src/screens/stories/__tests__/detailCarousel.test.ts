import { carouselCounter, carouselIndex, carouselPositionLabel } from '../detailCarousel';

describe('carouselCounter', () => {
  it('draws a one-based position out of the total', () => {
    expect(carouselCounter(0, 10)).toBe('1 / 10');
    expect(carouselCounter(4, 10)).toBe('5 / 10');
    expect(carouselCounter(9, 10)).toBe('10 / 10');
  });

  it('clamps an out-of-range index instead of drawing a nonsense position', () => {
    expect(carouselCounter(-3, 4)).toBe('1 / 4');
    expect(carouselCounter(99, 4)).toBe('4 / 4');
  });

  it('draws nothing when there are no pages', () => {
    expect(carouselCounter(0, 0)).toBe('');
    expect(carouselCounter(0, -2)).toBe('');
    expect(carouselCounter(0, Number.NaN)).toBe('');
  });
});

describe('carouselIndex', () => {
  it('maps a settled offset to the nearest page', () => {
    expect(carouselIndex(0, 390, 5)).toBe(0);
    expect(carouselIndex(390, 390, 5)).toBe(1);
    expect(carouselIndex(780, 390, 5)).toBe(2);
    // Mid-swipe rounding.
    expect(carouselIndex(600, 390, 5)).toBe(2);
  });

  it('clamps an overscroll past either end', () => {
    expect(carouselIndex(-40, 390, 5)).toBe(0);
    expect(carouselIndex(9999, 390, 5)).toBe(4);
  });

  it('reports the first page before layout or with no pages', () => {
    expect(carouselIndex(200, 0, 5)).toBe(0);
    expect(carouselIndex(0, Number.NaN, 5)).toBe(0);
    expect(carouselIndex(0, 390, 0)).toBe(0);
    expect(carouselIndex(Number.NaN, 390, 5)).toBe(0);
  });
});

describe('carouselPositionLabel', () => {
  it('names an image a photograph and a video a video', () => {
    expect(carouselPositionLabel('image', 1, 10)).toBe('Photograph 2 of 10');
    expect(carouselPositionLabel('video', 3, 10)).toBe('Video 4 of 10');
  });

  it('clamps the position and survives no pages', () => {
    expect(carouselPositionLabel('image', 99, 3)).toBe('Photograph 3 of 3');
    expect(carouselPositionLabel('video', 0, 0)).toBe('Video');
  });
});
