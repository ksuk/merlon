import { cn } from "@/lib/utils";
import type { ComponentProps } from "react";

type BrandLogoProps = Omit<
  ComponentProps<"img">,
  "src" | "alt" | "width" | "height"
>;

export function BrandLogo({ className, ...props }: BrandLogoProps) {
  return (
    <img
      src="/logo.svg"
      alt="Merlon"
      width={800}
      height={200}
      className={cn("block h-auto w-auto", className)}
      {...props}
    />
  );
}
