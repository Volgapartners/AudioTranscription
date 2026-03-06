import OpenAI from 'openai';

export const openai = process.env.OPENAI_API_KEY
  ? new OpenAI({
      apiKey: process.env.OPENAI_API_KEY,
      timeout: 120000,
      ...(process.env.OPENAI_BASE_URL && {
        baseURL: process.env.OPENAI_BASE_URL,
      }),
    })
  : null;
