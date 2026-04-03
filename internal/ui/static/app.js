// Valet UI — ~50 lines of custom JS for clipboard, auto-mask, dark mode.

// Reveal + auto-mask timer
function handleReveal(btn, key) {
  const maskEl = document.getElementById('mask-' + key);
  const revealEl = document.getElementById('reveal-' + key);

  // Show reveal, hide mask
  if (maskEl) maskEl.classList.add('hidden');
  if (revealEl) revealEl.classList.remove('hidden');

  // Start 30s countdown
  let remaining = 30;
  btn.disabled = true;
  btn.textContent = remaining + 's';
  btn.classList.add('bg-indigo-600', 'text-white');
  btn.classList.remove('bg-slate-700', 'text-slate-300');

  const timer = setInterval(() => {
    remaining--;
    btn.textContent = remaining + 's';
    if (remaining <= 0) {
      clearInterval(timer);
      // Re-mask
      if (maskEl) maskEl.classList.remove('hidden');
      if (revealEl) {
        revealEl.classList.add('hidden');
        revealEl.innerHTML = '';
      }
      btn.disabled = false;
      btn.textContent = 'Reveal';
      btn.classList.remove('bg-indigo-600', 'text-white');
      btn.classList.add('bg-slate-700', 'text-slate-300');
    }
  }, 1000);
}

// Copy from hidden target
function copyFromTarget() {
  const target = document.getElementById('copy-target');
  if (!target) return;

  const code = target.querySelector('code');
  const text = code ? code.textContent : target.textContent;
  if (text) {
    navigator.clipboard.writeText(text.trim()).then(() => {
      showToast('Copied to clipboard');
      target.innerHTML = '';
    });
  }
}

// Toast notifications
function showToast(message, type) {
  const container = document.getElementById('toast-container');
  if (!container) return;

  const toast = document.createElement('div');
  toast.className = 'bg-slate-800 border border-slate-700 rounded-lg px-4 py-3 shadow-lg text-sm text-white flex items-center gap-2';
  toast.style.animation = 'slideUp 200ms ease-out';

  const icon = type === 'error' ? '<span class="text-rose-400">&#10007;</span>'
    : '<span class="text-emerald-400">&#10003;</span>';
  toast.innerHTML = icon + '<span>' + message + '</span>';

  container.appendChild(toast);
  setTimeout(() => {
    toast.style.animation = 'slideDown 200ms ease-in forwards';
    setTimeout(() => toast.remove(), 200);
  }, 3000);
}

// Prefix validation for setup form inputs
function validatePrefix(input) {
  const expected = input.dataset.prefix;
  if (!expected) return;

  const status = input.nextElementSibling;
  if (!status) return;

  const value = input.value.trim();
  if (!value) {
    status.classList.add('hidden');
    input.classList.remove('border-emerald-500', 'border-amber-500');
    input.classList.add('border-slate-700');
    return;
  }

  // Extract just the prefix part (before "...")
  const prefix = expected.replace('...', '');
  status.classList.remove('hidden');

  if (value.startsWith(prefix)) {
    status.innerHTML = '<span class="text-emerald-400">&#10003;</span>';
    input.classList.remove('border-slate-700', 'border-amber-500');
    input.classList.add('border-emerald-500');
  } else {
    status.innerHTML = '<span class="text-amber-400">&#9888;</span>';
    input.classList.remove('border-slate-700', 'border-emerald-500');
    input.classList.add('border-amber-500');
  }
}

// Listen for htmx afterSwap to trigger toast on certain responses
document.addEventListener('htmx:afterSwap', function(evt) {
  // Check if the swapped content contains a success message
  const target = evt.detail.target;
  if (target && target.querySelector && target.querySelector('.text-emerald-400')) {
    // Success swap happened
  }
});
