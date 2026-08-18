import { StyleSheet, Text, View } from 'react-native';

// The brand mark: "Survivor League" set in the scary-drip Creepster
// treatment, stacked in two lines inside a black football badge with a
// stitched lace divider between them. Mirrors
// web/src/components/BrandWordmark.tsx's exact geometry — size is the
// football's width, everything else is derived from it.
export function BrandWordmark({ size = 96 }: { size?: number }) {
  const width = size;
  const height = size * 0.68;
  const laceWidth = size * 0.22;

  return (
    <View style={[styles.badge, { width, height, borderRadius: width / 2 }]}>
      <Text style={[styles.text, { fontSize: size * 0.145 }]}>SURVIVOR</Text>
      <View style={{ width: laceWidth, height: 1, marginVertical: 3, backgroundColor: 'rgba(255,255,255,0.8)' }} />
      <Text style={[styles.text, { fontSize: size * 0.185 }]}>LEAGUE</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  badge: {
    backgroundColor: '#000',
    alignItems: 'center',
    justifyContent: 'center',
  },
  text: {
    fontFamily: 'Creepster_400Regular',
    color: '#b91c1c',
    letterSpacing: 1,
    textShadowColor: '#000',
    textShadowOffset: { width: 0, height: 2 },
    textShadowRadius: 4,
  },
});
