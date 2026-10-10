function detectOS() {
  const ua = window.navigator.userAgent;
  const platform = window.navigator?.userAgentData?.platform || window.navigator.platform || '';

  if (/Win/i.test(platform) || /Win/i.test(ua)) {
    return 'windows';
  }
  
  if (/Mac/i.test(platform) || /Mac/i.test(ua)) {
    const isArm = (navigator.maxTouchPoints > 0) || /ARM/i.test(ua) || (window.screen.width >= 1000 && !/Intel/i.test(navigator.userAgent));
    return isArm ? 'macos-arm' : 'macos-intel';
  }
  
  if (/Linux/i.test(platform) || /Linux/i.test(ua)) {
    if (/arm/i.test(ua) || /aarch64/i.test(ua)) return 'linux-arm';
    if (/Ubuntu|Debian/i.test(ua)) return 'linux-deb';
    return 'linux';
  }

  return 'windows'; 
}

function highlightRecommendedDownload() {
  const userOS = detectOS();
  const cards = document.querySelectorAll('.download-card[data-os]');
  let matched = false;

  cards.forEach(card => {
    const cardOS = card.dataset.os;
    if (cardOS === userOS || (!matched && userOS.startsWith('macos') && cardOS.startsWith('macos'))) {
      card.classList.add('recommended');
      if (!card.querySelector('.recommended-badge')) {
        const badge = document.createElement('div');
        badge.className = 'recommended-badge';
        badge.innerHTML = '✨ Recommended';
        card.appendChild(badge);
      }
      matched = true;
    }
  });

  const docsTabs = document.querySelectorAll('.docs-tab');
  if (docsTabs.length > 0) {
    let targetTabId = 'windows-install';
    if (userOS.startsWith('macos')) targetTabId = 'macos-install';
    if (userOS.startsWith('linux')) targetTabId = 'linux-install';

    const targetBtn = document.querySelector(`.docs-tab[onclick*="${targetTabId}"]`);
    if (targetBtn) {
      switchTab(targetBtn, targetTabId);
    }
  }
}

document.addEventListener('DOMContentLoaded', () => {
  highlightRecommendedDownload();
});

function copyCode(btn) {
  const codeBlock = btn.nextElementSibling;
  const code = codeBlock.querySelector('code').textContent;
  navigator.clipboard.writeText(code).then(() => {
    const originalText = btn.textContent;
    btn.textContent = '✓ Copied';
    btn.classList.add('copied');
    setTimeout(() => {
      btn.textContent = originalText;
      btn.classList.remove('copied');
    }, 2000);
  });
}

function switchTab(btn, targetId) {
  const parent = btn.parentElement;
  if (!parent) return;
  const tabs = parent.querySelectorAll('.docs-tab');
  tabs.forEach(t => t.classList.remove('active'));
  btn.classList.add('active');

  const contents = parent.parentElement.querySelectorAll('.docs-tab-content');
  contents.forEach(c => c.classList.remove('active'));
  document.getElementById(targetId)?.classList.add('active');
}

document.addEventListener('DOMContentLoaded', () => {
  const navLinks = document.querySelectorAll('.docs-nav-link');

  navLinks.forEach(link => {
    link.addEventListener('click', (e) => {
      const href = link.getAttribute('href');
      if (href && href.startsWith('#')) {
        e.preventDefault();
        const target = document.querySelector(href);
        if (target) {
          navLinks.forEach(l => l.classList.remove('active'));
          link.classList.add('active');
          target.scrollIntoView({ behavior: 'smooth', block: 'start' });
        }
      }
    });
  });
});