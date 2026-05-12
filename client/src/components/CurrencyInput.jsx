import { useState, useEffect } from 'react';

// Formats a raw number string with thousand separators (no currency symbol).
// Keeps the raw numeric value in sync with the parent via value/onChange.
// onChange fires with a synthetic event-like object: { target: { value: rawString } }
export default function CurrencyInput({ value, onChange, placeholder = '0', style, ...props }) {
  const [display, setDisplay] = useState('');

  // Sync display when value changes externally (e.g. form reset, edit load)
  useEffect(() => {
    if (value === '' || value === undefined || value === null) {
      setDisplay('');
      return;
    }
    const num = Number(String(value).replace(/\./g, '').replace(',', '.'));
    if (!isNaN(num)) {
      setDisplay(num.toLocaleString('id-ID'));
    }
  }, [value]);

  const handleChange = (e) => {
    // Strip anything that isn't a digit or comma (decimal separator in id-ID)
    const raw = e.target.value.replace(/[^0-9,]/g, '');
    // Allow at most one comma
    const parts = raw.split(',');
    const cleaned = parts.length > 2 ? parts[0] + ',' + parts.slice(1).join('') : raw;

    // Parse to a number to reformat
    const numeric = cleaned.replace(/\./g, '').replace(',', '.');
    const num = parseFloat(numeric);

    if (cleaned === '' || cleaned === ',') {
      setDisplay(cleaned);
      onChange({ target: { value: '' } });
      return;
    }

    if (isNaN(num)) return;

    // Reformat integer part with separators, preserve decimal part as-is
    const [intPart, decPart] = cleaned.split(',');
    const intFormatted = parseInt(intPart.replace(/\./g, '') || '0', 10).toLocaleString('id-ID');
    const newDisplay = decPart !== undefined ? intFormatted + ',' + decPart : intFormatted;

    setDisplay(newDisplay);
    // Pass raw numeric string back (dot as decimal separator, no thousand sep)
    onChange({ target: { value: numeric } });
  };

  const handleBlur = () => {
    if (value === '' || value === undefined) return;
    const num = Number(String(value).replace(',', '.'));
    if (!isNaN(num)) setDisplay(num.toLocaleString('id-ID'));
  };

  return (
    <input
      {...props}
      type="text"
      inputMode="numeric"
      value={display}
      onChange={handleChange}
      onBlur={handleBlur}
      placeholder={placeholder}
      style={{ textAlign: 'right', ...style }}
    />
  );
}
