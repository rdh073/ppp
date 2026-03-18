/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  theme: {
    extend: {
      boxShadow: {
        soft: '0 16px 32px rgba(27, 77, 57, 0.14)',
      },
      fontFamily: {
        sans: ['Sora', 'Manrope', 'Trebuchet MS', 'sans-serif'],
      },
    },
  },
  plugins: [],
};
