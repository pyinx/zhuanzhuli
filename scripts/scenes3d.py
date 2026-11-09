"""Original offline 3D nature scenes. Run with Blender --background --python ... -- forest.

Blender is an authoring dependency only. The app ships PNG/H.264, never a 3D runtime.
One Blender unit is one metre; the character is 1.65 m and walks along the ground.
"""
import bpy, math, random, sys
from pathlib import Path
from mathutils import Vector

ROOT=Path(__file__).resolve().parents[1]
args=sys.argv[sys.argv.index('--')+1:] if '--' in sys.argv else ['forest']
theme=args[0]; preview='--preview' in args
if theme not in ('forest','beach','meadow','snow'):raise ValueError('unknown scene')
OUT=ROOT/'.build/scenes3d'/theme;OUT.mkdir(parents=True,exist_ok=True)
bpy.ops.object.select_all(action='SELECT');bpy.ops.object.delete(use_global=False)
scene=bpy.context.scene;scene.render.engine='BLENDER_EEVEE'
scene.render.resolution_x=1000;scene.render.resolution_y=420;scene.render.resolution_percentage=100
scene.render.image_settings.file_format='PNG';scene.render.fps=24
scene.world.use_nodes=True
background=next(n for n in scene.world.node_tree.nodes if n.type=='BACKGROUND')
background.inputs['Color'].default_value=(.64,.75,.70,1)
background.inputs['Strength'].default_value=.8
scene.view_settings.view_transform='AgX'
rng=random.Random(197+['forest','beach','meadow','snow'].index(theme))

def material(name,color,roughness=.85):
    m=bpy.data.materials.new(name);m.diffuse_color=(*color,1);m.use_nodes=True
    m.node_tree.nodes.clear();p=m.node_tree.nodes.new('ShaderNodeBsdfPrincipled');output=m.node_tree.nodes.new('ShaderNodeOutputMaterial');m.node_tree.links.new(p.outputs['BSDF'],output.inputs['Surface'])
    p.inputs['Base Color'].default_value=(*color,1);p.inputs['Roughness'].default_value=roughness
    return m
grass=material('soft sage',(.28,.42,.24));sand=material('warm sand',(.68,.53,.34))
snow=material('snow',(.75,.84,.85));bark=material('bark',(.20,.13,.08))
leaves=[material('foliage '+str(i),c) for i,c in enumerate([(.12,.26,.17),(.22,.36,.21),(.33,.46,.28)])]
stone=material('stone',(.39,.43,.40));water=material('quiet water',(.13,.38,.44),.3)
path=material('path',(.59,.48,.33));skin=material('skin',(.57,.35,.22));coat=material('linen jacket',(.75,.64,.43))
trousers=material('trousers',(.12,.19,.18));hair=material('hair',(.10,.075,.055))

def finish(o,name,mat,parent=None):
    o.name=name;o.data.materials.append(mat)
    if parent:o.parent=parent
    return o
def sphere(name,loc,scale,mat,parent=None):
    bpy.ops.mesh.primitive_uv_sphere_add(segments=16,ring_count=8,location=loc)
    o=bpy.context.object;o.scale=scale
    for p in o.data.polygons:p.use_smooth=True
    return finish(o,name,mat,parent)
def cone(name,loc,r1,r2,h,mat,parent=None):
    bpy.ops.mesh.primitive_cone_add(vertices=16,radius1=r1,radius2=r2,depth=h,location=loc)
    o=bpy.context.object
    for p in o.data.polygons:p.use_smooth=True
    return finish(o,name,mat,parent)
def plane(name,loc,scale,mat):
    bpy.ops.mesh.primitive_plane_add(size=2,location=loc);o=bpy.context.object;o.scale=scale
    return finish(o,name,mat)
def tree(x,y,size=1,pine=False):
    cone('trunk',(x,y,1.6*size),.18*size,.11*size,3.2*size,bark)
    if pine:
        for z,r in [(2.1,1.25),(2.9,1),(3.6,.75)]:cone('evergreen',(x,y,z*size),r*size,0,1.9*size,leaves[0])
    else:
        for dx,dy,z,r in [(0,0,3.4,1.35),(-.65,.1,3.1,1),(.6,.25,3.65,1.05)]:
            sphere('canopy',(x+dx*size,y+dy*size,z*size),(r*size,r*.8*size,r*size),leaves[rng.randrange(3)])
def line_mesh(name,points,width,mat):
    curve=bpy.data.curves.new(name,'CURVE');curve.dimensions='3D';curve.bevel_depth=width;curve.bevel_resolution=2
    s=curve.splines.new('POLY');s.points.add(len(points)-1)
    for p,v in zip(s.points,points):p.co=(*v,1)
    o=bpy.data.objects.new(name,curve);scene.collection.objects.link(o);curve.materials.append(mat);return o

ground=snow if theme=='snow' else sand if theme=='beach' else grass
plane('ground',(0,5,-.035),(80,80,1),ground)
# The open central path is the visual invitation, framed by scenery rather than UI.
if theme!='beach':
    verts=[]
    for y in [-12,-4,1,6,12,100]:
        center=.9*math.sin(y*.12);width=1.0+.015*max(0,-y)
        verts.extend([(center-width,y,.005),(center+width,y,.005)])
    mesh=bpy.data.meshes.new('path');mesh.from_pydata(verts,[],[(i,i+1,i+3,i+2) for i in range(0,10,2)]);mesh.update()
    ob=bpy.data.objects.new('walking path',mesh);scene.collection.objects.link(ob);mesh.materials.append(path if theme!='snow' else material('snow trail',(.61,.73,.76)))
if theme=='forest':
    for y in [-5,0,5,10,16,23,31]:
        for side in [-1,1]:tree(side*rng.uniform(3.5,7),y+rng.uniform(-1,1),rng.uniform(.85,1.3))
elif theme=='beach':
    plane('sea',(0,18,-.018),(80,12,1),water)
    foam=material('foam',(.67,.80,.77))
    for y in [6.3,7.3,9,12]:
        line_mesh('quiet wave',[(x,y+.16*math.sin(x*.45+y),.015) for x in range(-30,31)],.035,foam)
    for x,y in [(-5,-1),(-7,2),(7,3)]:
        sphere('coastal rock',(x,y,.30),(1.4,.8,.35),stone)
    # A small dune edge gives a grounded near/far cue without moving the camera.
    for x in [-12,-10,10,12]:sphere('dune',(x,0,-.45),(4,9,1.2),sand)
elif theme=='meadow':
    for x,y,s in [(-9,9,1.3),(8,15,1.1),(-12,22,1)]:tree(x,y,s)
    for i in range(90):
        x,y=rng.uniform(-13,13),rng.uniform(-5,19)
        if abs(x-.9*math.sin(y*.12))<1.5:continue
        cone('grass stem',(x,y,.15),.015,.009,.3,grass)
        sphere('small meadow flower',(x,y,.32),(.07,.07,.05),coat if i%3 else snow)
else:
    for x,y in [(-5,-2),(5,3),(-6,9),(7,14),(-5,20),(5,27)]:tree(x,y,rng.uniform(.9,1.25),True)
# Rounded distant ridges establish depth and keep every horizon free of hard seams.
ridge=material('distant ridge',(.31,.43,.39) if theme!='snow' else (.53,.66,.71))
for x,y,s in [(-20,42,9),(-4,49,13),(15,46,11),(30,51,10)]:sphere('distant hills',(x,y,-2),(s,10,s*.55),ridge)

bpy.ops.object.light_add(type='AREA',location=(-6,-4,16));key=bpy.context.object;key.name='broad daylight';key.data.energy=2300;key.data.shape='DISK';key.data.size=12
key.rotation_euler=(Vector((0,4,0))-key.location).to_track_quat('-Z','Y').to_euler()
bpy.ops.object.light_add(type='AREA',location=(9,8,10));fill=bpy.context.object;fill.name='sky fill';fill.data.energy=1100;fill.data.size=15
fill.rotation_euler=(Vector((0,5,0))-fill.location).to_track_quat('-Z','Y').to_euler()
bpy.ops.object.camera_add(location=(3,-16,5.2));camera=bpy.context.object
camera.rotation_euler=(Vector((0,10,1.5))-camera.location).to_track_quat('-Z','Y').to_euler();camera.data.lens=32;scene.camera=camera
scene.render.filepath=str(OUT/'background.png');bpy.ops.render.render(write_still=True)
if preview:sys.exit(0)

# Joined shapes share their joint transform. Feet meet the ground during stance;
# the figure walks away, so it never becomes a continuously moving work background.
rig=bpy.data.objects.new('walking figure',None);scene.collection.objects.link(rig)
sphere('body',(0,0,1.08),(.24,.15,.34),coat,rig)
sphere('head',(0,0,1.53),(.14,.135,.17),skin,rig)
sphere('hair',(0,-.025,1.63),(.145,.13,.09),hair,rig)
limbs=[]
for side in [-1,1]:
    leg=bpy.data.objects.new('hip',None);scene.collection.objects.link(leg);leg.parent=rig;leg.location=(side*.105,0,.82)
    sphere('leg',(0,0,-.32),(.09,.09,.35),trousers,leg)
    sphere('shoe',(0,.055,-.72),(.10,.16,.075),trousers,leg)
    arm=bpy.data.objects.new('shoulder',None);scene.collection.objects.link(arm);arm.parent=rig;arm.location=(side*.26,0,1.28)
    sphere('sleeve',(0,0,-.22),(.075,.075,.25),coat,arm)
    sphere('hand',(0,0,-.48),(.065,.06,.075),skin,arm)
    limbs.append((side,leg,arm))
scene.frame_start=1;scene.frame_end=144
for frame in range(1,145):
    t=(frame-1)/143;travel=6*t
    rig.location=(.9*math.sin((-2+travel)*.12),-2+travel,.02+.025*abs(math.sin(t*math.pi*14)))
    rig.keyframe_insert(data_path='location',frame=frame)
    for side,leg,arm in limbs:
        leg.rotation_euler.x=side*.22*math.sin(t*math.pi*14)
        arm.rotation_euler.x=-side*.20*math.sin(t*math.pi*14)
        leg.keyframe_insert(data_path='rotation_euler',frame=frame);arm.keyframe_insert(data_path='rotation_euler',frame=frame)
scene.render.filepath=str(OUT/'frame-');bpy.ops.wm.save_as_mainfile(filepath=str(OUT/'scene.blend'))
bpy.ops.render.render(animation=True)
