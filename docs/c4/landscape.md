<!-- GENERATED first-cut — lucos_repos ADR-0006, produced by prototype-generator.py on 2026-06-14.
     Connected core only (systems with at least one edge); the full 41-node graph is a hairball in Mermaid —
     see model.dsl for the model of record. Solid = sync (/_info dependsOn); dotted = async (loganne events). -->
# lucOS estate — connected core (generated)

```mermaid
flowchart LR
  lucos_arachne["lucos_arachne"]
  lucos_authentication["lucos_authentication"]
  lucos_backups["lucos_backups"]
  lucos_configy["lucos_configy"]
  lucos_contacts["lucos_contacts"]
  lucos_eolas["lucos_eolas"]
  lucos_loganne["lucos_loganne"]
  lucos_media_manager["lucos_media_manager"]
  lucos_media_metadata_api["lucos_media_metadata_api"]
  lucos_media_metadata_manager["lucos_media_metadata_manager"]
  lucos_media_seinn["lucos_media_seinn"]
  lucos_media_weightings["lucos_media_weightings"]
  lucos_monitoring["lucos_monitoring"]
  lucos_photos["lucos_photos"]
  lucos_time["lucos_time"]
  %% sync deps (solid)
  lucos_authentication --> lucos_contacts
  lucos_backups --> lucos_configy
  lucos_loganne --> lucos_arachne
  lucos_loganne --> lucos_media_manager
  lucos_loganne --> lucos_media_metadata_api
  lucos_loganne --> lucos_media_weightings
  lucos_loganne --> lucos_monitoring
  lucos_loganne --> lucos_photos
  lucos_media_metadata_manager --> lucos_media_metadata_api
  lucos_media_seinn --> lucos_media_manager
  lucos_media_weightings --> lucos_media_metadata_api
  lucos_media_weightings --> lucos_time
  lucos_time --> lucos_contacts
  lucos_time --> lucos_eolas
  %% async events (dotted, via loganne)
  lucos_loganne -.albumCreated.-> lucos_arachne
  lucos_loganne -.albumDeleted.-> lucos_arachne
  lucos_loganne -.albumUpdated.-> lucos_arachne
  lucos_loganne -.contactCreated.-> lucos_arachne
  lucos_loganne -.contactDeleted.-> lucos_arachne
  lucos_loganne -.contactUpdated.-> lucos_arachne
  lucos_loganne -.itemCreated.-> lucos_arachne
  lucos_loganne -.itemDeleted.-> lucos_arachne
  lucos_loganne -.itemUpdated.-> lucos_arachne
  lucos_loganne -.trackAdded.-> lucos_arachne
  lucos_loganne -.trackDeleted.-> lucos_arachne
  lucos_loganne -.trackUpdated.-> lucos_arachne
  lucos_loganne -.collectionCreated.-> lucos_media_manager
  lucos_loganne -.collectionDeleted.-> lucos_media_manager
  lucos_loganne -.collectionUpdated.-> lucos_media_manager
  lucos_loganne -.trackDeleted.-> lucos_media_manager
  lucos_loganne -.trackUpdated.-> lucos_media_manager
  lucos_loganne -.contactLinked.-> lucos_media_metadata_api
  lucos_loganne -.itemDeleted.-> lucos_media_metadata_api
  lucos_loganne -.itemMerged.-> lucos_media_metadata_api
  lucos_loganne -.itemUpdated.-> lucos_media_metadata_api
  lucos_loganne -.trackAdded.-> lucos_media_weightings
  lucos_loganne -.trackUpdated.-> lucos_media_weightings
  lucos_loganne -.deploySystem.-> lucos_monitoring
  lucos_loganne -.contactUpdated.-> lucos_photos
```
