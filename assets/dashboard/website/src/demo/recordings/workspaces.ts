import type { TerminalRecording } from './types';

const CSI = '\x1b[';
const CYAN = `${CSI}36m`;
const GREEN = `${CSI}32m`;
const DIM = `${CSI}2m`;
const BOLD = `${CSI}1m`;
const RESET = `${CSI}0m`;
const YELLOW = `${CSI}33m`;

export const authImplRecording: TerminalRecording = {
  sessionId: 'demo-sess-1',
  frames: [
    {
      delay: 0,
      data: `${DIM}$ agent --prompt "Implement user authentication with JWT"${RESET}\n`,
    },
    { delay: 600, data: `\n${CYAN}●${RESET} Analyzing codebase structure...\n` },
    { delay: 1200, data: `${DIM}  Reading src/middleware/...${RESET}\n` },
    { delay: 800, data: `${DIM}  Reading src/models/user.ts...${RESET}\n` },
    { delay: 1000, data: `${DIM}  Reading src/routes/...${RESET}\n` },
    { delay: 1500, data: `\n${CYAN}●${RESET} Creating auth middleware...\n` },
    { delay: 2000, data: `${GREEN}✓${RESET} Created ${BOLD}src/middleware/auth.ts${RESET}\n` },
    { delay: 800, data: `${GREEN}✓${RESET} Created ${BOLD}src/lib/jwt.ts${RESET}\n` },
    { delay: 1200, data: `\n${CYAN}●${RESET} Adding login and register routes...\n` },
    { delay: 2500, data: `${GREEN}✓${RESET} Created ${BOLD}src/routes/auth.ts${RESET}\n` },
    { delay: 1000, data: `${GREEN}✓${RESET} Updated ${BOLD}src/routes/index.ts${RESET}\n` },
    { delay: 1500, data: `\n${CYAN}●${RESET} Writing tests...\n` },
    {
      delay: 3000,
      data: `${GREEN}✓${RESET} Created ${BOLD}src/middleware/auth.test.ts${RESET} ${DIM}(12 tests)${RESET}\n`,
    },
    {
      delay: 2000,
      data: `${GREEN}✓${RESET} Created ${BOLD}src/routes/auth.test.ts${RESET} ${DIM}(8 tests)${RESET}\n`,
    },
    { delay: 1000, data: `\n${YELLOW}Running tests...${RESET}\n` },
    { delay: 2000, data: `${GREEN}✓${RESET} 20/20 tests passed\n` },
    {
      delay: 500,
      data: `\n${GREEN}${BOLD}Done.${RESET} Authentication system implemented with JWT.\n`,
    },
  ],
};

export const testWriterRecording: TerminalRecording = {
  sessionId: 'demo-sess-2',
  frames: [
    {
      delay: 0,
      data:
        `${DIM}$ agent --prompt "Write integration tests for the auth endpoints"${RESET}\n` +
        `\n${CYAN}●${RESET} Reading existing test patterns...\n` +
        `${DIM}  Found vitest config, supertest setup${RESET}\n` +
        `\n${CYAN}●${RESET} Planning integration tests...\n` +
        `${DIM}  • POST /auth/register - creates user and returns token${RESET}\n` +
        `${DIM}  • POST /auth/login - validates credentials, returns JWT${RESET}\n` +
        `${DIM}  • GET /auth/me - requires valid token${RESET}\n` +
        `${DIM}  • POST /auth/refresh - extends session${RESET}\n` +
        `${DIM}  • DELETE /auth/logout - revokes token${RESET}\n` +
        `\n${YELLOW}${BOLD}⛔ Needs Input:${RESET} ${YELLOW}Approve this test plan? (y/n)${RESET} `,
    },
  ],
};

export const rateLimiterRecording: TerminalRecording = {
  sessionId: 'demo-sess-3',
  frames: [
    {
      delay: 0,
      data:
        `${DIM}$ agent --prompt "Fix rate limiting bug in API gateway"${RESET}\n` +
        `\n${CYAN}●${RESET} Analyzing rate limiter implementation...\n` +
        `${DIM}  Reading src/middleware/rate-limiter.ts...${RESET}\n` +
        `${DIM}  Reading src/config/limits.ts...${RESET}\n` +
        `\n${CYAN}●${RESET} Found issue: sliding window not resetting correctly\n` +
        `${GREEN}✓${RESET} Fixed ${BOLD}src/middleware/rate-limiter.ts${RESET}\n` +
        `${GREEN}✓${RESET} Updated ${BOLD}src/middleware/rate-limiter.test.ts${RESET} ${DIM}(+3 tests)${RESET}\n` +
        `\n${YELLOW}Running tests...${RESET}\n` +
        `${GREEN}✓${RESET} 11/11 tests passed\n` +
        `\n${GREEN}${BOLD}Done.${RESET} Rate limiter sliding window fix applied.\n`,
    },
  ],
};
