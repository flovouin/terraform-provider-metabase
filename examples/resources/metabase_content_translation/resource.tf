# Basic content translation dictionary
resource "metabase_content_translation" "main" {
  dictionary = <<-EOT
Locale Code,String,Translation
en,Dashboard,Dashboard
fr,Dashboard,Tableau de bord
es,Dashboard,Tablero
en,Card,Card
fr,Card,Carte
es,Card,Tarjeta
en,Collection,Collection
fr,Collection,Collection
es,Collection,Colección
en,Database,Database
fr,Database,Base de données
es,Database,Base de datos
en,Table,Table
fr,Table,Table
es,Table,Tabla
en,Field,Field
fr,Field,Champ
es,Field,Campo
en,Question,Question
fr,Question,Question
es,Question,Pregunta
EOT
}

# Content translation from file
resource "metabase_content_translation" "from_file" {
  dictionary = file("${path.module}/translations.csv")
}
