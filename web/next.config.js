/** @type {import('next').NextConfig} */
const path = require('path');
require('dotenv').config({ path: path.join(__dirname, '.env') });

const nextConfig = {
  reactStrictMode: true,
};

module.exports = nextConfig;
