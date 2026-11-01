import {
  ANSWER_BODY_MAX_LENGTH,
  INQUIRY_BODY_MAX_LENGTH,
  INQUIRY_TITLE_MAX_LENGTH,
  NEARBY_RECIPIENT_LIMIT,
  answerCountLabel,
  answerDraftError,
  filterInquiries,
  inquiryAudienceLabel,
  inquiryDraftError,
  inquiryPlaceLabel,
} from '../inquiry';

describe('answerCountLabel', () => {
  it('names the empty state rather than showing a zero', () => {
    expect(answerCountLabel(0)).toBe('No answers yet');
  });

  it('uses the singular for exactly one', () => {
    expect(answerCountLabel(1)).toBe('1 answer');
  });

  it('uses the plural beyond one', () => {
    expect(answerCountLabel(3)).toBe('3 answers');
  });

  it('treats a negative or non-finite count as empty', () => {
    expect(answerCountLabel(-1)).toBe('No answers yet');
    expect(answerCountLabel(Number.NaN)).toBe('No answers yet');
  });
});

describe('inquiryPlaceLabel', () => {
  it('names the place', () => {
    expect(inquiryPlaceLabel('Manguzi')).toBe('About Manguzi');
  });

  it('trims the place', () => {
    expect(inquiryPlaceLabel('  Manguzi  ')).toBe('About Manguzi');
  });

  it('reads a place-less question as a statement, not a missing value', () => {
    expect(inquiryPlaceLabel(null)).toBe('No place named');
    expect(inquiryPlaceLabel(undefined)).toBe('No place named');
    expect(inquiryPlaceLabel('   ')).toBe('No place named');
  });
});

describe('inquiryAudienceLabel', () => {
  it('says who is told when a place is named', () => {
    const label = inquiryAudienceLabel('Manguzi');
    expect(label).toContain(`first ${NEARBY_RECIPIENT_LIMIT} people rooted in Manguzi`);
    expect(label).toContain('Inquiries list');
  });

  it('says plainly that a place-less question notifies nobody', () => {
    const label = inquiryAudienceLabel(null);
    expect(label).toContain('nobody is notified');
    expect(label).toContain('still public');
  });
});

describe('filterInquiries', () => {
  const questions = [
    {
      title: 'Why do the older women wear red ribbons?',
      body: 'At the festival almost every woman over sixty had one.',
      place: 'Manguzi',
      place_country: 'South Africa',
    },
    {
      title: 'Who keeps the drums?',
      body: 'The elders do.',
      place: 'Durban',
      place_country: 'South Africa',
    },
    {
      title: 'Where is the salt?',
      body: 'In the pans.',
      place: null,
      place_country: null,
    },
  ];

  it('keeps every question for an empty phrase, which is what closing the field does', () => {
    expect(filterInquiries(questions, '')).toHaveLength(3);
    expect(filterInquiries(questions, '   ')).toHaveLength(3);
  });

  it('matches the title, ignoring case', () => {
    expect(filterInquiries(questions, 'RIBBONS').map((question) => question.title)).toEqual([
      'Why do the older women wear red ribbons?',
    ]);
  });

  it('matches the question and the place as well as the title', () => {
    expect(filterInquiries(questions, 'elders').map((question) => question.title)).toEqual([
      'Who keeps the drums?',
    ]);
    expect(filterInquiries(questions, 'manguzi').map((question) => question.title)).toEqual([
      'Why do the older women wear red ribbons?',
    ]);
  });

  it('matches the country a place belongs to', () => {
    expect(filterInquiries(questions, 'south africa')).toHaveLength(2);
  });

  it('narrows as a partial word is typed', () => {
    expect(filterInquiries(questions, 'rib')).toHaveLength(1);
  });

  it('returns nothing when no question matches', () => {
    expect(filterInquiries(questions, 'hips')).toEqual([]);
  });

  it('tolerates missing fields and a phrase that is not a string', () => {
    const sparse = [{ title: null, body: undefined }];

    expect(filterInquiries(sparse, 'anything')).toEqual([]);
    expect(filterInquiries(questions, 42 as unknown as string)).toHaveLength(3);
  });
});

describe('inquiryDraftError', () => {
  const valid = { title: 'Why do the cattle come home?', body: 'Every evening.', language: 'eng' };

  it('accepts a complete draft', () => {
    expect(inquiryDraftError(valid)).toBeUndefined();
  });

  it('requires a title', () => {
    expect(inquiryDraftError({ ...valid, title: '   ' })).toBe('A question title is required.');
  });

  it('bounds the title', () => {
    const title = 'a'.repeat(INQUIRY_TITLE_MAX_LENGTH + 1);
    expect(inquiryDraftError({ ...valid, title })).toBe(
      `The title must be at most ${INQUIRY_TITLE_MAX_LENGTH} characters.`,
    );
  });

  it('accepts a title exactly at the maximum', () => {
    const title = 'a'.repeat(INQUIRY_TITLE_MAX_LENGTH);
    expect(inquiryDraftError({ ...valid, title })).toBeUndefined();
  });

  it('requires a question body', () => {
    expect(inquiryDraftError({ ...valid, body: '\n\t ' })).toBe('A question is required.');
  });

  it('bounds the body', () => {
    const body = 'a'.repeat(INQUIRY_BODY_MAX_LENGTH + 1);
    expect(inquiryDraftError({ ...valid, body })).toBe(
      `The question must be at most ${INQUIRY_BODY_MAX_LENGTH} characters.`,
    );
  });

  it('requires a language', () => {
    expect(inquiryDraftError({ ...valid, language: '' })).toBe('A language is required.');
  });
});

describe('answerDraftError', () => {
  it('accepts a real answer', () => {
    expect(answerDraftError('They follow the river.')).toBeUndefined();
  });

  it('requires an answer', () => {
    expect(answerDraftError('  ')).toBe('An answer is required.');
  });

  it('bounds the answer', () => {
    const body = 'a'.repeat(ANSWER_BODY_MAX_LENGTH + 1);
    expect(answerDraftError(body)).toBe(
      `The answer must be at most ${ANSWER_BODY_MAX_LENGTH} characters.`,
    );
  });
});
