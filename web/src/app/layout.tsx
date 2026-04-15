import type { Metadata } from "next";
import { Space_Grotesk, JetBrains_Mono } from "next/font/google";
import "./globals.css";

const spaceGrotesk = Space_Grotesk({
  variable: "--font-sans",
  subsets: ["latin"],
});

const jetbrains = JetBrains_Mono({
  variable: "--font-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "VSAT",
  description:
    "Real-time internship and job monitoring across 50+ top tech companies.",
  openGraph: {
    title: "VSAT",
    description: "Real-time internship and job monitoring across 50+ top tech companies.",
    url: "https://www.vsat.dev",
    siteName: "VSAT",
    type: "website",
  },
  twitter: {
    card: "summary_large_image",
    title: "VSAT",
    description: "Real-time internship and job monitoring across 50+ top tech companies.",
  },
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html
      lang="en"
      className={`${spaceGrotesk.variable} ${jetbrains.variable} antialiased`}
    >
      <body>{children}</body>
    </html>
  );
}
