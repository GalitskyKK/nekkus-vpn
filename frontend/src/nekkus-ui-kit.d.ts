/** Fallback types when @nekkus/ui-kit is not yet installed or dist is missing. Remove after npm install. */
declare module "@nekkus/ui-kit" {
  import type { FC, HTMLAttributes, ButtonHTMLAttributes, InputHTMLAttributes, SelectHTMLAttributes } from "react";

  export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
    variant?: "primary" | "secondary" | "ghost" | "danger";
    size?: "sm" | "md" | "large";
    children: React.ReactNode;
  }
  export const Button: FC<ButtonProps>;

  export interface CardProps extends HTMLAttributes<HTMLDivElement> {
    children: React.ReactNode;
    title?: string;
    accentTop?: boolean;
  }
  export const Card: FC<CardProps>;

  export interface PageLayoutProps {
    children: React.ReactNode;
    className?: string;
    style?: React.CSSProperties;
  }
  export const PageLayout: FC<PageLayoutProps>;

  export interface SectionProps extends HTMLAttributes<HTMLElement> {
    children: React.ReactNode;
    title?: string;
    className?: string;
  }
  export const Section: FC<SectionProps>;

  export interface PillProps extends HTMLAttributes<HTMLSpanElement> {
    children: React.ReactNode;
    variant?: "default" | "success" | "warning" | "error" | "info";
  }
  export const Pill: FC<PillProps>;

  export interface SelectOption {
    value: string;
    label: string;
  }
  export interface SelectProps extends Omit<SelectHTMLAttributes<HTMLSelectElement>, "className"> {
    className?: string;
    label?: string;
    options: SelectOption[];
  }
  export const Select: FC<SelectProps>;

  export interface InputProps extends Omit<InputHTMLAttributes<HTMLInputElement>, "className"> {
    className?: string;
    label?: string;
  }
  export const Input: FC<InputProps>;

  export interface StatusDotProps {
    status: "online" | "offline" | "busy" | "error";
    label?: string;
    size?: number;
    pulse?: boolean;
  }
  export const StatusDot: FC<StatusDotProps>;
}
