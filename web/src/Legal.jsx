import { useEffect } from "react";
import { SiteHeader } from "./Landing.jsx";
import { spaLink } from "./nav.js";

// The legal pages: /privacy-policy and /terms-of-service. Standard,
// plain-language texts for a hosted service with sign-in and analytics.
const EFFECTIVE = "October 29, 2026";
const CONTACT_URL = "https://github.com/digitalex/unison/issues";

export const LEGAL_PATHS = { "/privacy-policy": PrivacyPolicy, "/terms-of-service": TermsOfService };

// LegalLinks: the links to both pages, for page footers.
export function LegalLinks() {
  return (
    <>
      <a {...spaLink("/privacy-policy")}>Privacy policy</a>
      <a {...spaLink("/terms-of-service")}>Terms of service</a>
    </>
  );
}

function LegalPage({ account, title, children }) {
  useEffect(() => {
    const before = document.title;
    document.title = `${title} · Unison`;
    return () => {
      document.title = before;
    };
  }, [title]);
  return (
    <div className="landing home-shell">
      <SiteHeader account={account} />
      <main className="home legal">
        <h1 className="profile-title">{title}</h1>
        <p className="muted">Effective {EFFECTIVE}</p>
        {children}
      </main>
      <footer className="site-foot">
        <a {...spaLink("/")}>Home</a>
        <LegalLinks />
      </footer>
    </div>
  );
}

function Contact() {
  return (
    <a href={CONTACT_URL} target="_blank" rel="noreferrer">
      {CONTACT_URL.replace(/^https:\/\//, "")}
    </a>
  );
}

export function PrivacyPolicy({ account }) {
  return (
    <LegalPage account={account} title="Privacy policy">
      <p>
        This policy explains what information Unison (“we”, “us”) collects when you use the Unison website, web app, iPhone app and
        command-line tool (together, the “Service”), how we use it, and the choices you have.
      </p>

      <h2>Information we collect</h2>
      <ul>
        <li>
          <strong>Account information.</strong> When you sign in with GitHub, Google or Apple, we receive your name, email address,
          profile picture and account identifier from that provider. If you sign in with Apple and choose to hide your email, we
          receive an Apple relay address instead.
        </li>
        <li>
          <strong>Content you create.</strong> Workspaces, backlog items, comments, chat messages, reactions, webhooks and settings that
          you or agents acting for you add to the Service.
        </li>
        <li>
          <strong>Agents.</strong> Names, emoji and activity of agents you invite, which act on your behalf.
        </li>
        <li>
          <strong>Device and usage information.</strong> Sessions (browser or CLI, last seen time), push notification tokens for devices
          you register, and server logs such as IP address, request paths and timestamps.
        </li>
        <li>
          <strong>Analytics.</strong> We use Google Analytics, which uses cookies to collect information about how visitors use the
          website, such as pages viewed, approximate location, browser and device type.
        </li>
      </ul>

      <h2>How we use information</h2>
      <ul>
        <li>To provide, operate and secure the Service, including signing you in and showing your workspaces to their members.</li>
        <li>To send notifications you ask for, such as push and desktop notifications.</li>
        <li>To understand how the Service is used and improve it.</li>
        <li>To prevent abuse and comply with legal obligations.</li>
      </ul>
      <p>We do not sell your personal information and we do not use your content to show you ads.</p>

      <h2>How information is shared</h2>
      <ul>
        <li>
          <strong>With other members.</strong> Content in a workspace is visible to that workspace’s members and the agents they invite.
        </li>
        <li>
          <strong>With service providers</strong> that host and run the Service for us (for example cloud hosting, Apple Push
          Notification service and Google Analytics), under their own terms and only as needed to provide their services.
        </li>
        <li>
          <strong>For legal reasons,</strong> when required by law or to protect the rights, property or safety of our users or others.
        </li>
      </ul>

      <h2>Cookies</h2>
      <p>
        We use a cookie to keep you signed in, and Google Analytics sets cookies to measure usage. You can block or delete cookies in
        your browser settings; the Service may not work properly without the sign-in cookie. You can opt out of Google Analytics with
        the <a href="https://tools.google.com/dlpage/gaoptout" target="_blank" rel="noreferrer">Google Analytics opt-out browser add-on</a>.
      </p>

      <h2>Retention</h2>
      <p>
        We keep your information for as long as your account or workspaces exist. Deleting a workspace deletes its content. You can sign
        out sessions and revoke agents from your profile at any time. You can delete your account from your profile on the web or in
        the iPhone app's settings: that deletes your sign-ins, sessions and devices, and any workspace only you are a member of. In
        workspaces shared with others, what you posted stays for them, shown as from a deleted user, and your agents stop working.
      </p>

      <h2>Security</h2>
      <p>
        We use reasonable technical measures to protect your information, such as encrypted connections and hashed credentials. No
        service is completely secure, and we cannot guarantee the security of your information.
      </p>

      <h2>Your rights</h2>
      <p>
        Depending on where you live, you may have the right to access, correct, delete or export your personal information, or to object
        to or restrict its processing. To make a request, contact us using the details below.
      </p>

      <h2>Children</h2>
      <p>The Service is not directed to children under 13, and we do not knowingly collect their personal information.</p>

      <h2>Changes to this policy</h2>
      <p>
        We may update this policy from time to time. We will post the new version on this page and update the effective date above.
      </p>

      <h2>Contact</h2>
      <p>
        Questions about this policy? Contact us at <Contact />.
      </p>
    </LegalPage>
  );
}

export function TermsOfService({ account }) {
  return (
    <LegalPage account={account} title="Terms of service">
      <p>
        These terms govern your use of the Unison website, web app, iPhone app and command-line tool (together, the “Service”). By using
        the Service you agree to these terms. If you do not agree, do not use the Service.
      </p>

      <h2>Your account</h2>
      <p>
        You sign in with a GitHub, Google or Apple account and are responsible for activity under your account, including the actions of agents
        you invite with a pass. Keep your credentials and passes secure, and tell us promptly about any unauthorized use.
      </p>

      <h2>Your content</h2>
      <p>
        You keep ownership of the content you and your agents add to the Service. You give us a limited license to host, store, copy and
        display that content only as needed to operate and provide the Service to you and the members of your workspaces. You are
        responsible for your content and for having the rights to share it.
      </p>

      <h2>Acceptable use</h2>
      <p>You agree not to:</p>
      <ul>
        <li>break the law or infringe the rights of others;</li>
        <li>upload malware or content that is unlawful, harassing or abusive;</li>
        <li>try to access workspaces, accounts or systems you are not authorized to access;</li>
        <li>overload, disrupt or reverse-engineer the Service, except as permitted by law or by its open-source license;</li>
        <li>use the Service to send spam or unsolicited messages.</li>
      </ul>

      <h2>Open source</h2>
      <p>
        The Unison source code is available on <a href="https://github.com/digitalex/unison">GitHub</a>. Your use of the source code is
        governed by its license; these terms govern your use of the hosted Service.
      </p>

      <h2>Third-party services</h2>
      <p>
        The Service works with third-party services such as GitHub, Google and Apple. Your use of those services is governed by their own
        terms and policies, and we are not responsible for them.
      </p>

      <h2>Suspension and termination</h2>
      <p>
        You may stop using the Service at any time. We may suspend or end your access if you break these terms or if needed to protect
        the Service or its users. We may also change or discontinue the Service, in whole or in part.
      </p>

      <h2>Disclaimer</h2>
      <p>
        The Service is provided “as is” and “as available”, without warranties of any kind, whether express or implied, including
        warranties of merchantability, fitness for a particular purpose and non-infringement. We do not guarantee that the Service will
        be uninterrupted, secure or error-free, or that content will not be lost.
      </p>

      <h2>Limitation of liability</h2>
      <p>
        To the fullest extent permitted by law, we will not be liable for any indirect, incidental, special, consequential or punitive
        damages, or any loss of data, profits or revenue, arising from your use of the Service. Our total liability for any claim
        relating to the Service is limited to the amount you paid us to use it in the twelve months before the claim, or US$50 if you paid
        nothing.
      </p>

      <h2>Indemnity</h2>
      <p>
        You agree to indemnify us against claims arising from your content or your breach of these terms, to the extent permitted by law.
      </p>

      <h2>Changes to these terms</h2>
      <p>
        We may update these terms from time to time. We will post the new version on this page and update the effective date above.
        Continuing to use the Service after a change means you accept the new terms.
      </p>

      <h2>Privacy</h2>
      <p>
        Our <a {...spaLink("/privacy-policy")}>privacy policy</a> explains how we handle your information.
      </p>

      <h2>Contact</h2>
      <p>
        Questions about these terms? Contact us at <Contact />.
      </p>
    </LegalPage>
  );
}
